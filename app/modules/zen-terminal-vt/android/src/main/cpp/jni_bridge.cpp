#include <jni.h>
#include <android/log.h>
#include <array>
#include <cmath>
#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>
#include <ghostty/vt.h>

#define TAG "ZenTerminalVt"
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, TAG, __VA_ARGS__)
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, TAG, __VA_ARGS__)

static constexpr size_t kDefaultScrollbackRows = 10000;
static constexpr size_t kMouseStackBufferSize = 128;

static jstring newStringFromUtf8Bytes(JNIEnv* env, const uint8_t* bytes, size_t len) {
    jclass stringClass = env->FindClass("java/lang/String");
    if (!stringClass) {
        return env->NewStringUTF("");
    }

    jmethodID ctor = env->GetMethodID(stringClass, "<init>", "([BLjava/lang/String;)V");
    if (!ctor) {
        return env->NewStringUTF("");
    }

    jbyteArray byteArray = env->NewByteArray((jsize)len);
    if (!byteArray) {
        return env->NewStringUTF("");
    }

    if (len > 0) {
        env->SetByteArrayRegion(
            byteArray,
            0,
            (jsize)len,
            reinterpret_cast<const jbyte*>(bytes)
        );
    }

    jstring charset = env->NewStringUTF("UTF-8");
    if (!charset) {
        env->DeleteLocalRef(byteArray);
        return env->NewStringUTF("");
    }

    auto result = static_cast<jstring>(env->NewObject(stringClass, ctor, byteArray, charset));
    env->DeleteLocalRef(byteArray);
    env->DeleteLocalRef(charset);

    if (env->ExceptionCheck() || !result) {
        env->ExceptionClear();
        return env->NewStringUTF("");
    }

    return result;
}

/**
 * Per-terminal state: owns the terminal, render state, and formatters.
 */
struct TerminalHandle {
    GhosttyTerminal terminal = nullptr;
    GhosttyRenderState render_state = nullptr;
    GhosttyFormatter plain_formatter = nullptr;
    GhosttyFormatter html_formatter = nullptr;
    GhosttyMouseEncoder mouse_encoder = nullptr;
    uint16_t cols = 0;
    uint16_t rows = 0;
    uint32_t cell_width_px = 1;
    uint32_t cell_height_px = 1;
    bool force_full_snapshot = true;
};

static GhosttyResult createTerminalFormatter(
    TerminalHandle* h,
    GhosttyFormatterFormat format,
    GhosttyFormatter* out)
{
    if (!h || !out) {
        return GHOSTTY_INVALID_VALUE;
    }

    GhosttyFormatterTerminalOptions opts = GHOSTTY_INIT_SIZED(GhosttyFormatterTerminalOptions);
    opts.emit = format;
    opts.trim = format == GHOSTTY_FORMATTER_FORMAT_PLAIN;

    return ghostty_formatter_terminal_new(nullptr, out, h->terminal, opts);
}

static jstring formatTerminalScreen(
    JNIEnv* env,
    TerminalHandle* h,
    GhosttyFormatterFormat format)
{
    if (!h) return env->NewStringUTF("");

    GhosttyFormatter formatter =
        format == GHOSTTY_FORMATTER_FORMAT_HTML ? h->html_formatter : h->plain_formatter;
    if (!formatter) {
        return env->NewStringUTF("");
    }

    uint8_t* outPtr = nullptr;
    size_t outLen = 0;
    GhosttyResult res = ghostty_formatter_format_alloc(formatter, nullptr, &outPtr, &outLen);

    if (res != GHOSTTY_SUCCESS || !outPtr) {
        return env->NewStringUTF("");
    }

    jstring result = newStringFromUtf8Bytes(env, outPtr, outLen);
    ghostty_free(nullptr, outPtr, outLen);
    return result;
}

static TerminalHandle* getHandle(jlong h) {
    return reinterpret_cast<TerminalHandle*>(h);
}

static void markFullSnapshot(TerminalHandle* h) {
    if (!h) {
        return;
    }
    h->force_full_snapshot = true;
}

static GhosttyResult populateRowIterator(
    GhosttyRenderState renderState,
    GhosttyRenderStateRowIterator rowIterator)
{
    if (!renderState || !rowIterator) {
        return GHOSTTY_INVALID_VALUE;
    }

    return ghostty_render_state_get(
        renderState,
        GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR,
        &rowIterator
    );
}

static GhosttyResult populateRowCells(
    GhosttyRenderStateRowIterator rowIterator,
    GhosttyRenderStateRowCells rowCells)
{
    if (!rowIterator || !rowCells) {
        return GHOSTTY_INVALID_VALUE;
    }

    return ghostty_render_state_row_get(
        rowIterator,
        GHOSTTY_RENDER_STATE_ROW_DATA_CELLS,
        &rowCells
    );
}

static jstring newStringFromStdString(JNIEnv* env, const std::string& value) {
    return newStringFromUtf8Bytes(
        env,
        reinterpret_cast<const uint8_t*>(value.data()),
        value.size()
    );
}

static bool colorsEqual(const GhosttyColorRgb& left, const GhosttyColorRgb& right) {
    return left.r == right.r && left.g == right.g && left.b == right.b;
}

static double colorPerceivedLuminance(const GhosttyColorRgb& color) {
    return (
        0.299 * static_cast<double>(color.r) +
        0.587 * static_cast<double>(color.g) +
        0.114 * static_cast<double>(color.b)
    ) / 255.0;
}

static uint8_t mixColorChannel(uint8_t from, uint8_t to, double weight) {
    const double mixed =
        static_cast<double>(from) +
        (static_cast<double>(to) - static_cast<double>(from)) * weight;
    if (mixed <= 0.0) {
        return 0;
    }
    if (mixed >= 255.0) {
        return 255;
    }
    return static_cast<uint8_t>(std::lround(mixed));
}

static GhosttyColorRgb mixColors(
    const GhosttyColorRgb& from,
    const GhosttyColorRgb& to,
    double weight)
{
    return GhosttyColorRgb{
        mixColorChannel(from.r, to.r, weight),
        mixColorChannel(from.g, to.g, weight),
        mixColorChannel(from.b, to.b, weight),
    };
}

static void appendCssHexColor(std::string* out, const GhosttyColorRgb& color) {
    static constexpr char hex[] = "0123456789abcdef";
    out->push_back('#');
    out->push_back(hex[(color.r >> 4) & 0xF]);
    out->push_back(hex[color.r & 0xF]);
    out->push_back(hex[(color.g >> 4) & 0xF]);
    out->push_back(hex[color.g & 0xF]);
    out->push_back(hex[(color.b >> 4) & 0xF]);
    out->push_back(hex[color.b & 0xF]);
}

static void appendUtf8(std::string* out, uint32_t codepoint) {
    if (codepoint > 0x10FFFF || (codepoint >= 0xD800 && codepoint <= 0xDFFF)) {
        codepoint = 0xFFFD;
    }

    if (codepoint <= 0x7F) {
        out->push_back(static_cast<char>(codepoint));
        return;
    }

    if (codepoint <= 0x7FF) {
        out->push_back(static_cast<char>(0xC0 | (codepoint >> 6)));
        out->push_back(static_cast<char>(0x80 | (codepoint & 0x3F)));
        return;
    }

    if (codepoint <= 0xFFFF) {
        out->push_back(static_cast<char>(0xE0 | (codepoint >> 12)));
        out->push_back(static_cast<char>(0x80 | ((codepoint >> 6) & 0x3F)));
        out->push_back(static_cast<char>(0x80 | (codepoint & 0x3F)));
        return;
    }

    out->push_back(static_cast<char>(0xF0 | (codepoint >> 18)));
    out->push_back(static_cast<char>(0x80 | ((codepoint >> 12) & 0x3F)));
    out->push_back(static_cast<char>(0x80 | ((codepoint >> 6) & 0x3F)));
    out->push_back(static_cast<char>(0x80 | (codepoint & 0x3F)));
}

static void appendHtmlEscapedCodepoint(std::string* out, uint32_t codepoint) {
    switch (codepoint) {
        case '&':
            out->append("&amp;");
            return;
        case '<':
            out->append("&lt;");
            return;
        case '>':
            out->append("&gt;");
            return;
        default:
            appendUtf8(out, codepoint);
            return;
    }
}

static void appendCellText(
    GhosttyRenderStateRowCells rowCells,
    GhosttyCell cell,
    bool preserveBlankCell,
    std::string* htmlText)
{
    if (!htmlText) {
        return;
    }

    uint32_t graphemeLen = 0;
    if (ghostty_render_state_row_cells_get(
            rowCells,
            GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_LEN,
            &graphemeLen) == GHOSTTY_SUCCESS &&
        graphemeLen > 0) {
        std::vector<uint32_t> graphemes(graphemeLen, 0);
        if (ghostty_render_state_row_cells_get(
                rowCells,
                GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_BUF,
                graphemes.data()) == GHOSTTY_SUCCESS) {
            for (uint32_t codepoint : graphemes) {
                appendHtmlEscapedCodepoint(htmlText, codepoint);
            }
            return;
        }
    }

    bool hasText = false;
    ghostty_cell_get(cell, GHOSTTY_CELL_DATA_HAS_TEXT, &hasText);
    if (!hasText) {
        if (preserveBlankCell) {
            htmlText->push_back(' ');
        }
        return;
    }

    uint32_t codepoint = 0;
    ghostty_cell_get(cell, GHOSTTY_CELL_DATA_CODEPOINT, &codepoint);
    if (codepoint == 0) {
        if (preserveBlankCell) {
            htmlText->push_back(' ');
        }
        return;
    }

    appendHtmlEscapedCodepoint(htmlText, codepoint);
}

static bool htmlHasVisibleText(const std::string& html) {
    bool inTag = false;
    for (size_t index = 0; index < html.size(); index += 1) {
        const char ch = html[index];
        if (inTag) {
            if (ch == '>') {
                inTag = false;
            }
            continue;
        }

        if (ch == '<') {
            inTag = true;
            continue;
        }

        if (ch == '&') {
            const size_t entityEnd = html.find(';', index + 1);
            if (entityEnd == std::string::npos) {
                return true;
            }

            const std::string entity = html.substr(index, entityEnd - index + 1);
            if (entity != "&nbsp;" && entity != "&#32;" && entity != "&#x20;") {
                return true;
            }
            index = entityEnd;
            continue;
        }

        if (ch != ' ') {
            return true;
        }
    }

    return false;
}

static bool resolveStyleColor(
    const GhosttyStyleColor& color,
    const GhosttyRenderStateColors& renderColors,
    GhosttyColorRgb* out)
{
    if (!out) {
        return false;
    }

    switch (color.tag) {
        case GHOSTTY_STYLE_COLOR_NONE:
            return false;
        case GHOSTTY_STYLE_COLOR_PALETTE:
            *out = renderColors.palette[color.value.palette];
            return true;
        case GHOSTTY_STYLE_COLOR_RGB:
            *out = color.value.rgb;
            return true;
        default:
            return false;
    }
}

static GhosttyColorRgb resolveEffectiveForeground(
    const GhosttyStyle& style,
    const GhosttyRenderStateColors& renderColors,
    bool hasResolvedFg,
    const GhosttyColorRgb& resolvedFg)
{
    if (style.fg_color.tag == GHOSTTY_STYLE_COLOR_PALETTE) {
        uint8_t paletteIndex = style.fg_color.value.palette;
        if (style.bold && paletteIndex <= GHOSTTY_COLOR_NAMED_WHITE) {
            paletteIndex = static_cast<uint8_t>(paletteIndex + 8);
        }
        return renderColors.palette[paletteIndex];
    }

    if (style.fg_color.tag == GHOSTTY_STYLE_COLOR_RGB) {
        return style.fg_color.value.rgb;
    }

    return hasResolvedFg ? resolvedFg : renderColors.foreground;
}

static bool resolveEffectiveBackground(
    const GhosttyStyle& style,
    const GhosttyRenderStateColors& renderColors,
    bool hasResolvedBg,
    const GhosttyColorRgb& resolvedBg,
    GhosttyColorRgb* out)
{
    if (!out) {
        return false;
    }

    if (hasResolvedBg) {
        *out = resolvedBg;
        return true;
    }

    switch (style.bg_color.tag) {
        case GHOSTTY_STYLE_COLOR_PALETTE:
            *out = renderColors.palette[style.bg_color.value.palette];
            return true;
        case GHOSTTY_STYLE_COLOR_RGB:
            *out = style.bg_color.value.rgb;
            return true;
        case GHOSTTY_STYLE_COLOR_NONE:
        default:
            *out = renderColors.background;
            return false;
    }
}

static std::string buildCellCss(
    const GhosttyStyle& style,
    const GhosttyRenderStateColors& renderColors,
    bool hasResolvedFg,
    const GhosttyColorRgb& resolvedFg,
    bool hasResolvedBg,
    const GhosttyColorRgb& resolvedBg)
{
    GhosttyColorRgb fg =
        resolveEffectiveForeground(style, renderColors, hasResolvedFg, resolvedFg);
    GhosttyColorRgb bg = renderColors.background;
    bool hasBg = resolveEffectiveBackground(style, renderColors, hasResolvedBg, resolvedBg, &bg);

    if (style.inverse) {
        const bool hasExplicitFg =
            hasResolvedFg || style.fg_color.tag != GHOSTTY_STYLE_COLOR_NONE;
        const bool hasExplicitBg =
            hasBg || style.bg_color.tag != GHOSTTY_STYLE_COLOR_NONE;
        const bool isDefaultReverseOnLightTheme =
            !hasExplicitFg &&
            !hasExplicitBg &&
            colorPerceivedLuminance(renderColors.background) > 0.62;

        if (isDefaultReverseOnLightTheme) {
            const GhosttyColorRgb highlightSource =
                renderColors.cursor_has_value ? renderColors.cursor : renderColors.foreground;
            fg = renderColors.foreground;
            bg = mixColors(renderColors.background, highlightSource, 0.22);
        } else {
            const GhosttyColorRgb originalFg = fg;
            fg = bg;
            bg = originalFg;
        }
        hasBg = true;
    }

    std::string css;
    css.reserve(160);

    if (style.invisible) {
        css.append("color:transparent;");
    } else if (!colorsEqual(fg, renderColors.foreground)) {
        css.append("color:");
        appendCssHexColor(&css, fg);
        css.push_back(';');
    }

    if (hasBg && !colorsEqual(bg, renderColors.background)) {
        css.append("background-color:");
        appendCssHexColor(&css, bg);
        css.push_back(';');
    }

    if (style.bold) {
        css.append("font-weight:700;");
    }
    if (style.italic) {
        css.append("font-style:italic;");
    }
    if (style.faint) {
        css.append("opacity:0.72;");
    }

    std::string decorationLine;
    if (style.underline != GHOSTTY_SGR_UNDERLINE_NONE) {
        decorationLine.append(" underline");
    }
    if (style.strikethrough) {
        decorationLine.append(" line-through");
    }
    if (style.overline) {
        decorationLine.append(" overline");
    }
    if (!decorationLine.empty()) {
        css.append("text-decoration-line:");
        css.append(decorationLine.c_str() + 1);
        css.push_back(';');
    }

    if (style.underline != GHOSTTY_SGR_UNDERLINE_NONE) {
        css.append("text-decoration-style:");
        switch (style.underline) {
            case GHOSTTY_SGR_UNDERLINE_DOUBLE:
                css.append("double;");
                break;
            case GHOSTTY_SGR_UNDERLINE_CURLY:
                css.append("wavy;");
                break;
            case GHOSTTY_SGR_UNDERLINE_DOTTED:
                css.append("dotted;");
                break;
            case GHOSTTY_SGR_UNDERLINE_DASHED:
                css.append("dashed;");
                break;
            case GHOSTTY_SGR_UNDERLINE_SINGLE:
            default:
                css.append("solid;");
                break;
        }

        GhosttyColorRgb underlineColor = {};
        if (resolveStyleColor(style.underline_color, renderColors, &underlineColor)) {
            css.append("text-decoration-color:");
            appendCssHexColor(&css, underlineColor);
            css.push_back(';');
        }
    }

    return css;
}

static void flushStyledSegment(
    std::string* rowHtml,
    const std::string& css,
    std::string* text)
{
    if (text->empty()) {
        return;
    }

    if (css.empty()) {
        rowHtml->append(*text);
        text->clear();
        return;
    }

    rowHtml->append("<span style=\"");
    rowHtml->append(css);
    rowHtml->append("\">");
    rowHtml->append(*text);
    rowHtml->append("</span>");
    text->clear();
}

static std::string buildRowHtml(
    GhosttyRenderStateRowIterator rowIterator,
    const GhosttyRenderStateColors& renderColors)
{
    GhosttyRenderStateRowCells rowCells = nullptr;
    if (ghostty_render_state_row_cells_new(nullptr, &rowCells) != GHOSTTY_SUCCESS || !rowCells) {
        return "";
    }

    const GhosttyResult rowCellsRes = populateRowCells(rowIterator, rowCells);
    if (rowCellsRes != GHOSTTY_SUCCESS) {
        ghostty_render_state_row_cells_free(rowCells);
        return "";
    }

    std::string rowHtml;
    std::string segmentCss;
    std::string segmentText;
    rowHtml.reserve(256);
    segmentText.reserve(128);
    bool sawVisibleText = false;
    bool sawNonDefaultBackground = false;

    while (ghostty_render_state_row_cells_next(rowCells)) {
        GhosttyCell cell = 0;
        if (ghostty_render_state_row_cells_get(
                rowCells,
                GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_RAW,
                &cell) != GHOSTTY_SUCCESS) {
            continue;
        }

        GhosttyCellWide wide = GHOSTTY_CELL_WIDE_NARROW;
        ghostty_cell_get(cell, GHOSTTY_CELL_DATA_WIDE, &wide);
        if (wide == GHOSTTY_CELL_WIDE_SPACER_TAIL || wide == GHOSTTY_CELL_WIDE_SPACER_HEAD) {
            continue;
        }

        GhosttyStyle style = GHOSTTY_INIT_SIZED(GhosttyStyle);
        ghostty_style_default(&style);
        ghostty_render_state_row_cells_get(
            rowCells,
            GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE,
            &style
        );

        GhosttyColorRgb fg = renderColors.foreground;
        const bool hasFg =
            ghostty_render_state_row_cells_get(
                rowCells,
                GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_FG_COLOR,
                &fg) == GHOSTTY_SUCCESS;
        if (!hasFg) {
            fg = renderColors.foreground;
        }

        GhosttyColorRgb bg = renderColors.background;
        const bool hasBg =
            ghostty_render_state_row_cells_get(
                rowCells,
                GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_BG_COLOR,
                &bg) == GHOSTTY_SUCCESS;

        const std::string css = buildCellCss(style, renderColors, hasFg, fg, hasBg, bg);
        if (!segmentText.empty() && css != segmentCss) {
            flushStyledSegment(&rowHtml, segmentCss, &segmentText);
        }
        segmentCss = css;

        const bool preserveBlankCell = true;
        if (css.find("background-color:") != std::string::npos) {
            sawNonDefaultBackground = true;
        }

        const size_t previousLen = segmentText.size();
        appendCellText(rowCells, cell, preserveBlankCell, &segmentText);
        if (!sawVisibleText && segmentText.size() > previousLen) {
            bool hasText = false;
            ghostty_cell_get(cell, GHOSTTY_CELL_DATA_HAS_TEXT, &hasText);
            uint32_t codepoint = 0;
            ghostty_cell_get(cell, GHOSTTY_CELL_DATA_CODEPOINT, &codepoint);
            if (hasText && codepoint != 0 && codepoint != ' ') {
                sawVisibleText = true;
            }
        }
    }

    flushStyledSegment(&rowHtml, segmentCss, &segmentText);
    ghostty_render_state_row_cells_free(rowCells);
    if (!sawNonDefaultBackground && !sawVisibleText && !htmlHasVisibleText(rowHtml)) {
        return "";
    }
    return rowHtml;
}

static bool buildVisibleHtml(
    GhosttyRenderState renderState,
    uint16_t expectedRows,
    std::string* out)
{
    if (!renderState || !out) {
        return false;
    }

    GhosttyRenderStateColors renderColors = GHOSTTY_INIT_SIZED(GhosttyRenderStateColors);
    ghostty_render_state_colors_get(renderState, &renderColors);

    GhosttyRenderStateRowIterator rowIterator = nullptr;
    if (ghostty_render_state_row_iterator_new(nullptr, &rowIterator) != GHOSTTY_SUCCESS || !rowIterator) {
        return false;
    }

    const GhosttyResult rowIteratorRes = populateRowIterator(renderState, rowIterator);
    if (rowIteratorRes != GHOSTTY_SUCCESS) {
        ghostty_render_state_row_iterator_free(rowIterator);
        return false;
    }

    out->clear();
    out->reserve(static_cast<size_t>(expectedRows) * 96);

    uint16_t rowIndex = 0;
    while (ghostty_render_state_row_iterator_next(rowIterator)) {
        if (rowIndex > 0) {
            out->push_back('\n');
        }
        out->append(buildRowHtml(rowIterator, renderColors));
        rowIndex += 1;
    }

    ghostty_render_state_row_iterator_free(rowIterator);
    return rowIndex == expectedRows;
}

static void clearRenderStateDirty(GhosttyRenderState renderState) {
    if (!renderState) {
        return;
    }

    GhosttyRenderStateDirty clean = GHOSTTY_RENDER_STATE_DIRTY_FALSE;
    ghostty_render_state_set(renderState, GHOSTTY_RENDER_STATE_OPTION_DIRTY, &clean);

    GhosttyRenderStateRowIterator rowIterator = nullptr;
    if (ghostty_render_state_row_iterator_new(nullptr, &rowIterator) != GHOSTTY_SUCCESS || !rowIterator) {
        return;
    }

    if (populateRowIterator(renderState, rowIterator) != GHOSTTY_SUCCESS) {
        ghostty_render_state_row_iterator_free(rowIterator);
        return;
    }

    const bool cleanRow = false;
    while (ghostty_render_state_row_iterator_next(rowIterator)) {
        ghostty_render_state_row_set(
            rowIterator,
            GHOSTTY_RENDER_STATE_ROW_OPTION_DIRTY,
            &cleanRow
        );
    }

    ghostty_render_state_row_iterator_free(rowIterator);
}

static uint32_t roundPositivePixels(jfloat value) {
    if (!std::isfinite(value) || value <= 0) {
        return 1;
    }

    const long rounded = std::lround(value);
    return rounded > 0 ? static_cast<uint32_t>(rounded) : 1;
}

static uint32_t safeScreenPixels(uint16_t cells, uint32_t cellPixels) {
    const uint64_t screen = static_cast<uint64_t>(cells) * static_cast<uint64_t>(cellPixels);
    return screen > UINT32_MAX ? UINT32_MAX : static_cast<uint32_t>(screen);
}

static bool parseHexColor(const char* value, GhosttyColorRgb* out) {
    if (!value || !out) {
        return false;
    }

    const char* hex = value[0] == '#' ? value + 1 : value;
    if (std::strlen(hex) != 6) {
        return false;
    }

    char* end = nullptr;
    const unsigned long rgb = std::strtoul(hex, &end, 16);
    if (!end || end != hex + 6 || *end != '\0') {
        return false;
    }

    out->r = static_cast<uint8_t>((rgb >> 16) & 0xFF);
    out->g = static_cast<uint8_t>((rgb >> 8) & 0xFF);
    out->b = static_cast<uint8_t>(rgb & 0xFF);
    return true;
}

static bool parseHexColorString(JNIEnv* env, jstring value, GhosttyColorRgb* out) {
    if (!env || !value || !out) {
        return false;
    }

    const char* utf8 = env->GetStringUTFChars(value, nullptr);
    if (!utf8) {
        return false;
    }

    const bool ok = parseHexColor(utf8, out);
    env->ReleaseStringUTFChars(value, utf8);
    return ok;
}

static bool setTerminalOption(
    TerminalHandle* h,
    GhosttyTerminalOption option,
    const void* value,
    const char* label)
{
    if (!h || !h->terminal) {
        return false;
    }

    const GhosttyResult res = ghostty_terminal_set(h->terminal, option, value);
    if (res != GHOSTTY_SUCCESS) {
        LOGE("ghostty_terminal_set %s failed: %d", label, res);
        return false;
    }

    return true;
}

static bool decodeMouseAction(jint action, GhosttyMouseAction* out) {
    if (!out) {
        return false;
    }

    switch (action) {
        case 0:
            *out = GHOSTTY_MOUSE_ACTION_PRESS;
            return true;
        case 1:
            *out = GHOSTTY_MOUSE_ACTION_RELEASE;
            return true;
        case 2:
            *out = GHOSTTY_MOUSE_ACTION_MOTION;
            return true;
        default:
            return false;
    }
}

static bool decodeMouseButton(jint button, GhosttyMouseButton* out, bool* hasButton) {
    if (!out || !hasButton) {
        return false;
    }

    switch (button) {
        case 0:
            *hasButton = false;
            *out = GHOSTTY_MOUSE_BUTTON_UNKNOWN;
            return true;
        case 1:
            *hasButton = true;
            *out = GHOSTTY_MOUSE_BUTTON_LEFT;
            return true;
        case 2:
            *hasButton = true;
            *out = GHOSTTY_MOUSE_BUTTON_RIGHT;
            return true;
        case 3:
            *hasButton = true;
            *out = GHOSTTY_MOUSE_BUTTON_MIDDLE;
            return true;
        case 4:
            *hasButton = true;
            *out = GHOSTTY_MOUSE_BUTTON_FOUR;
            return true;
        case 5:
            *hasButton = true;
            *out = GHOSTTY_MOUSE_BUTTON_FIVE;
            return true;
        default:
            return false;
    }
}

static jstring encodeMouseSequence(
    JNIEnv* env,
    TerminalHandle* h,
    GhosttyMouseAction action,
    GhosttyMouseButton button,
    bool hasButton,
    jfloat x,
    jfloat y,
    GhosttyMods mods,
    bool anyButtonPressed)
{
    if (!h || !h->mouse_encoder) {
        return env->NewStringUTF("");
    }

    GhosttyMouseEncoderSize size = GHOSTTY_INIT_SIZED(GhosttyMouseEncoderSize);
    size.screen_width = safeScreenPixels(h->cols, h->cell_width_px);
    size.screen_height = safeScreenPixels(h->rows, h->cell_height_px);
    size.cell_width = h->cell_width_px;
    size.cell_height = h->cell_height_px;

    ghostty_mouse_encoder_setopt_from_terminal(h->mouse_encoder, h->terminal);
    ghostty_mouse_encoder_setopt(h->mouse_encoder, GHOSTTY_MOUSE_ENCODER_OPT_SIZE, &size);
    ghostty_mouse_encoder_setopt(
        h->mouse_encoder,
        GHOSTTY_MOUSE_ENCODER_OPT_ANY_BUTTON_PRESSED,
        &anyButtonPressed
    );

    if (action != GHOSTTY_MOUSE_ACTION_MOTION) {
        ghostty_mouse_encoder_reset(h->mouse_encoder);
    }

    GhosttyMouseEvent event = nullptr;
    GhosttyResult res = ghostty_mouse_event_new(nullptr, &event);
    if (res != GHOSTTY_SUCCESS || !event) {
        LOGE("ghostty_mouse_event_new failed: %d", res);
        return env->NewStringUTF("");
    }

    ghostty_mouse_event_set_action(event, action);
    if (hasButton) {
        ghostty_mouse_event_set_button(event, button);
    } else {
        ghostty_mouse_event_clear_button(event);
    }
    ghostty_mouse_event_set_mods(event, mods);
    ghostty_mouse_event_set_position(event, GhosttyMousePosition{ .x = x, .y = y });

    char stackBuffer[kMouseStackBufferSize] = {};
    size_t outLen = 0;
    res = ghostty_mouse_encoder_encode(
        h->mouse_encoder,
        event,
        stackBuffer,
        sizeof(stackBuffer),
        &outLen
    );

    if (res == GHOSTTY_OUT_OF_SPACE && outLen > sizeof(stackBuffer)) {
        std::string dynamicBuffer(outLen, '\0');
        res = ghostty_mouse_encoder_encode(
            h->mouse_encoder,
            event,
            dynamicBuffer.data(),
            dynamicBuffer.size(),
            &outLen
        );
        ghostty_mouse_event_free(event);
        if (res != GHOSTTY_SUCCESS || outLen == 0) {
            return env->NewStringUTF("");
        }
        return newStringFromUtf8Bytes(
            env,
            reinterpret_cast<const uint8_t*>(dynamicBuffer.data()),
            outLen
        );
    }

    ghostty_mouse_event_free(event);
    if (res != GHOSTTY_SUCCESS || outLen == 0) {
        return env->NewStringUTF("");
    }

    return newStringFromUtf8Bytes(env, reinterpret_cast<const uint8_t*>(stackBuffer), outLen);
}

extern "C" {

JNIEXPORT jlong JNICALL
Java_expo_modules_zenterminalvt_ZenTerminalVtModule_nativeCreateTerminal(
    JNIEnv* env, jobject, jint cols, jint rows)
{
    auto* h = new TerminalHandle();
    h->cols = (uint16_t)cols;
    h->rows = (uint16_t)rows;

    GhosttyTerminalOptions opts = {};
    opts.cols = h->cols;
    opts.rows = h->rows;
    opts.max_scrollback = kDefaultScrollbackRows;

    GhosttyResult res = ghostty_terminal_new(nullptr, &h->terminal, opts);
    if (res != GHOSTTY_SUCCESS) {
        LOGE("ghostty_terminal_new failed: %d", res);
        delete h;
        return 0;
    }

    res = ghostty_render_state_new(nullptr, &h->render_state);
    if (res != GHOSTTY_SUCCESS) {
        LOGE("ghostty_render_state_new failed: %d", res);
        ghostty_terminal_free(h->terminal);
        delete h;
        return 0;
    }

    res = createTerminalFormatter(h, GHOSTTY_FORMATTER_FORMAT_PLAIN, &h->plain_formatter);
    if (res != GHOSTTY_SUCCESS) {
        LOGE("plain formatter init failed: %d", res);
        ghostty_render_state_free(h->render_state);
        ghostty_terminal_free(h->terminal);
        delete h;
        return 0;
    }

    res = createTerminalFormatter(h, GHOSTTY_FORMATTER_FORMAT_HTML, &h->html_formatter);
    if (res != GHOSTTY_SUCCESS) {
        LOGE("html formatter init failed: %d", res);
        ghostty_formatter_free(h->plain_formatter);
        ghostty_render_state_free(h->render_state);
        ghostty_terminal_free(h->terminal);
        delete h;
        return 0;
    }

    res = ghostty_mouse_encoder_new(nullptr, &h->mouse_encoder);
    if (res != GHOSTTY_SUCCESS) {
        LOGE("mouse encoder init failed: %d", res);
        ghostty_formatter_free(h->html_formatter);
        ghostty_formatter_free(h->plain_formatter);
        ghostty_render_state_free(h->render_state);
        ghostty_terminal_free(h->terminal);
        delete h;
        return 0;
    }

    const bool trackLastCell = true;
    ghostty_mouse_encoder_setopt(
        h->mouse_encoder,
        GHOSTTY_MOUSE_ENCODER_OPT_TRACK_LAST_CELL,
        &trackLastCell
    );

    LOGI("Terminal created: %dx%d", cols, rows);
    return reinterpret_cast<jlong>(h);
}

JNIEXPORT void JNICALL
Java_expo_modules_zenterminalvt_ZenTerminalVtModule_nativeDestroyTerminal(
    JNIEnv*, jobject, jlong handle)
{
    auto* h = getHandle(handle);
    if (!h) return;

    ghostty_mouse_encoder_free(h->mouse_encoder);
    ghostty_formatter_free(h->html_formatter);
    ghostty_formatter_free(h->plain_formatter);
    ghostty_render_state_free(h->render_state);
    ghostty_terminal_free(h->terminal);
    delete h;
    LOGI("Terminal destroyed");
}

JNIEXPORT void JNICALL
Java_expo_modules_zenterminalvt_ZenTerminalVtModule_nativeWriteData(
    JNIEnv* env, jobject, jlong handle, jstring data)
{
    auto* h = getHandle(handle);
    if (!h) return;

    const char* utf8 = env->GetStringUTFChars(data, nullptr);
    if (!utf8) return;

    jsize len = env->GetStringUTFLength(data);
    ghostty_terminal_vt_write(
        h->terminal,
        reinterpret_cast<const uint8_t*>(utf8),
        (size_t)len
    );
    env->ReleaseStringUTFChars(data, utf8);
}

JNIEXPORT void JNICALL
Java_expo_modules_zenterminalvt_ZenTerminalVtModule_nativeScrollViewport(
    JNIEnv*, jobject, jlong handle, jint delta)
{
    auto* h = getHandle(handle);
    if (!h || delta == 0) return;

    GhosttyTerminalScrollViewport behavior = {};
    behavior.tag = GHOSTTY_SCROLL_VIEWPORT_DELTA;
    behavior.value.delta = static_cast<intptr_t>(delta);

    ghostty_terminal_scroll_viewport(h->terminal, behavior);
    markFullSnapshot(h);
}

JNIEXPORT void JNICALL
Java_expo_modules_zenterminalvt_ZenTerminalVtModule_nativeScrollViewportToBottom(
    JNIEnv*, jobject, jlong handle)
{
    auto* h = getHandle(handle);
    if (!h) return;

    GhosttyTerminalScrollViewport behavior = {};
    behavior.tag = GHOSTTY_SCROLL_VIEWPORT_BOTTOM;

    ghostty_terminal_scroll_viewport(h->terminal, behavior);
    markFullSnapshot(h);
}

JNIEXPORT void JNICALL
Java_expo_modules_zenterminalvt_ZenTerminalVtModule_nativeResize(
    JNIEnv*, jobject, jlong handle,
    jint cols, jint rows, jfloat cellWidth, jfloat cellHeight)
{
    auto* h = getHandle(handle);
    if (!h) return;

    h->cols = (uint16_t)cols;
    h->rows = (uint16_t)rows;
    h->cell_width_px = roundPositivePixels(cellWidth);
    h->cell_height_px = roundPositivePixels(cellHeight);

    ghostty_terminal_resize(
        h->terminal,
        h->cols,
        h->rows,
        h->cell_width_px,
        h->cell_height_px
    );
    markFullSnapshot(h);
}
JNIEXPORT void JNICALL
Java_expo_modules_zenterminalvt_ZenTerminalVtModule_nativeSetTheme(
    JNIEnv* env, jobject, jlong handle,
    jstring foreground, jstring background, jstring cursor, jobjectArray palette)
{
    auto* h = getHandle(handle);
    if (!h || !palette) return;

    GhosttyColorRgb fg = {};
    GhosttyColorRgb bg = {};
    GhosttyColorRgb cursorColor = {};
    if (!parseHexColorString(env, foreground, &fg) ||
        !parseHexColorString(env, background, &bg) ||
        !parseHexColorString(env, cursor, &cursorColor)) {
        LOGE("nativeSetTheme received an invalid theme color");
        return;
    }

    const jsize paletteLen = env->GetArrayLength(palette);
    if (paletteLen < 256) {
        LOGE("nativeSetTheme palette too small: %d", static_cast<int>(paletteLen));
        return;
    }

    std::array<GhosttyColorRgb, 256> paletteColors = {};
    for (jsize i = 0; i < 256; i += 1) {
        auto* entry = static_cast<jstring>(env->GetObjectArrayElement(palette, i));
        const bool ok = entry && parseHexColorString(env, entry, &paletteColors[static_cast<size_t>(i)]);
        if (entry) {
            env->DeleteLocalRef(entry);
        }
        if (!ok) {
            LOGE("nativeSetTheme invalid palette color at index %d", static_cast<int>(i));
            return;
        }
    }

    if (!setTerminalOption(h, GHOSTTY_TERMINAL_OPT_COLOR_FOREGROUND, &fg, "foreground") ||
        !setTerminalOption(h, GHOSTTY_TERMINAL_OPT_COLOR_BACKGROUND, &bg, "background") ||
        !setTerminalOption(h, GHOSTTY_TERMINAL_OPT_COLOR_CURSOR, &cursorColor, "cursor") ||
        !setTerminalOption(h, GHOSTTY_TERMINAL_OPT_COLOR_PALETTE, paletteColors.data(), "palette")) {
        return;
    }

    markFullSnapshot(h);
}

JNIEXPORT jstring JNICALL
Java_expo_modules_zenterminalvt_ZenTerminalVtModule_nativeEncodeMouseEvent(
    JNIEnv* env, jobject, jlong handle, jint action, jint button,
    jfloat x, jfloat y, jint mods, jboolean anyButtonPressed)
{
    auto* h = getHandle(handle);
    if (!h) {
        return env->NewStringUTF("");
    }

    GhosttyMouseAction decodedAction = GHOSTTY_MOUSE_ACTION_PRESS;
    if (!decodeMouseAction(action, &decodedAction)) {
        return env->NewStringUTF("");
    }

    GhosttyMouseButton decodedButton = GHOSTTY_MOUSE_BUTTON_UNKNOWN;
    bool hasButton = false;
    if (!decodeMouseButton(button, &decodedButton, &hasButton)) {
        return env->NewStringUTF("");
    }

    return encodeMouseSequence(
        env,
        h,
        decodedAction,
        decodedButton,
        hasButton,
        x,
        y,
        static_cast<GhosttyMods>(mods),
        anyButtonPressed == JNI_TRUE
    );
}

JNIEXPORT jobject JNICALL
Java_expo_modules_zenterminalvt_ZenTerminalVtModule_nativeGetRenderSnapshot(
    JNIEnv* env, jobject, jlong handle)
{
    auto* h = getHandle(handle);

    // Build result HashMap
    jclass mapClass = env->FindClass("java/util/HashMap");
    jmethodID mapInit = env->GetMethodID(mapClass, "<init>", "()V");
    jmethodID mapPut = env->GetMethodID(mapClass, "put",
        "(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;");
    jobject map = env->NewObject(mapClass, mapInit);

    jclass intClass = env->FindClass("java/lang/Integer");
    jmethodID intOf = env->GetStaticMethodID(intClass, "valueOf", "(I)Ljava/lang/Integer;");
    jclass boolClass = env->FindClass("java/lang/Boolean");
    jmethodID boolOf = env->GetStaticMethodID(boolClass, "valueOf", "(Z)Ljava/lang/Boolean;");

    auto putStr = [&](const char* key, const char* val) {
        env->CallObjectMethod(map, mapPut, env->NewStringUTF(key), env->NewStringUTF(val));
    };
    auto putJString = [&](const char* key, jstring val) {
        env->CallObjectMethod(map, mapPut, env->NewStringUTF(key), val);
    };
    auto putInt = [&](const char* key, jint val) {
        env->CallObjectMethod(map, mapPut, env->NewStringUTF(key),
            env->CallStaticObjectMethod(intClass, intOf, val));
    };
    auto putBool = [&](const char* key, bool val) {
        env->CallObjectMethod(map, mapPut, env->NewStringUTF(key),
            env->CallStaticObjectMethod(boolClass, boolOf, (jboolean)val));
    };

    if (!h) {
        putStr("dirty", "none");
        return map;
    }

    // Update render state from terminal before formatting the visible viewport.
    if (ghostty_render_state_update(h->render_state, h->terminal) != GHOSTTY_SUCCESS) {
        putStr("dirty", "none");
        return map;
    }

    uint16_t renderRows = h->rows;
    uint16_t renderCols = h->cols;
    ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_ROWS, &renderRows);
    ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_COLS, &renderCols);

    GhosttyRenderStateDirty dirty = GHOSTTY_RENDER_STATE_DIRTY_FALSE;
    ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_DIRTY, &dirty);
    if (h->force_full_snapshot) {
        dirty = GHOSTTY_RENDER_STATE_DIRTY_FULL;
    }

    const char* dirtyStr = "none";
    if (dirty == GHOSTTY_RENDER_STATE_DIRTY_PARTIAL) {
        dirtyStr = "partial";
    } else if (dirty == GHOSTTY_RENDER_STATE_DIRTY_FULL) {
        dirtyStr = "full";
    }

    putStr("dirty", dirtyStr);
    putInt("rows", (jint)renderRows);
    putInt("cols", (jint)renderCols);
    if (dirty == GHOSTTY_RENDER_STATE_DIRTY_FALSE) {
        return map;
    }

    // Cursor
    bool cursorInViewport = false;
    ghostty_render_state_get(h->render_state,
        GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_HAS_VALUE, &cursorInViewport);

    if (cursorInViewport) {
        uint16_t cx = 0, cy = 0;
        ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_X, &cx);
        ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_Y, &cy);
        putInt("cursorCol", (jint)cx);
        putInt("cursorRow", (jint)cy);

        bool cursorVisible = false;
        ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_CURSOR_VISIBLE, &cursorVisible);
        putBool("cursorVisible", cursorVisible);
    } else {
        putInt("cursorCol", 0);
        putInt("cursorRow", 0);
        putBool("cursorVisible", false);
    }

    std::string visibleHtml;
    if (buildVisibleHtml(h->render_state, renderRows, &visibleHtml)) {
        putJString("html", newStringFromStdString(env, visibleHtml));
    } else {
        putJString("html", formatTerminalScreen(env, h, GHOSTTY_FORMATTER_FORMAT_HTML));
    }

    h->force_full_snapshot = false;
    clearRenderStateDirty(h->render_state);

    return map;
}

JNIEXPORT jstring JNICALL
Java_expo_modules_zenterminalvt_ZenTerminalVtModule_nativeGetVisibleText(
    JNIEnv* env, jobject, jlong handle)
{
    auto* h = getHandle(handle);
    return formatTerminalScreen(env, h, GHOSTTY_FORMATTER_FORMAT_PLAIN);
}

JNIEXPORT jstring JNICALL
Java_expo_modules_zenterminalvt_ZenTerminalVtModule_nativeGetVisibleHtml(
    JNIEnv* env, jobject, jlong handle)
{
    auto* h = getHandle(handle);
    if (!h) {
        return env->NewStringUTF("");
    }

    if (ghostty_render_state_update(h->render_state, h->terminal) != GHOSTTY_SUCCESS) {
        return formatTerminalScreen(env, h, GHOSTTY_FORMATTER_FORMAT_HTML);
    }

    uint16_t renderRows = h->rows;
    ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_ROWS, &renderRows);

    std::string visibleHtml;
    if (buildVisibleHtml(h->render_state, renderRows, &visibleHtml)) {
        return newStringFromStdString(env, visibleHtml);
    }

    return formatTerminalScreen(env, h, GHOSTTY_FORMATTER_FORMAT_HTML);
}

} // extern "C"
