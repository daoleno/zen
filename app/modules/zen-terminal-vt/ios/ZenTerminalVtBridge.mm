#import "ZenTerminalVtBridge.h"

#include <cstdio>

#define LOGI(...) do { std::fprintf(stderr, "[ZenTerminalVt] "); std::fprintf(stderr, __VA_ARGS__); std::fprintf(stderr, "\n"); } while (0)
#define LOGE(...) do { std::fprintf(stderr, "[ZenTerminalVt] ERROR: "); std::fprintf(stderr, __VA_ARGS__); std::fprintf(stderr, "\n"); } while (0)

// The rendering and Ghostty state helpers are shared with Android. JNI-only
// declarations and entry points in this file are guarded by __ANDROID__.
#include "../android/src/main/cpp/jni_bridge.cpp"

namespace {

static NSString *NSStringFromBytes(const char *bytes, size_t length) {
    if (!bytes || length == 0) {
        return @"";
    }

    NSString *value = [[NSString alloc] initWithBytes:bytes
                                                length:length
                                              encoding:NSUTF8StringEncoding];
    return value ?: @"";
}

static NSString *NSStringFromStdString(const std::string &value) {
    return NSStringFromBytes(value.data(), value.size());
}

static TerminalHandle *TerminalFromHandle(unsigned long long handle) {
    return getHandle(static_cast<uintptr_t>(handle));
}

} // namespace

@implementation ZenTerminalVtBridge

+ (unsigned long long)createTerminalWithCols:(NSInteger)cols rows:(NSInteger)rows {
    if (cols <= 0 || rows <= 0 || cols > UINT16_MAX || rows > UINT16_MAX) {
        return 0;
    }

    auto *terminal = new TerminalHandle();
    terminal->cols = static_cast<uint16_t>(cols);
    terminal->rows = static_cast<uint16_t>(rows);

    GhosttyTerminalOptions options = {};
    options.cols = terminal->cols;
    options.rows = terminal->rows;
    options.max_scrollback = kDefaultScrollbackRows;

    GhosttyResult result = ghostty_terminal_new(nullptr, &terminal->terminal, options);
    if (result != GHOSTTY_SUCCESS) {
        LOGE("ghostty_terminal_new failed: %d", result);
        delete terminal;
        return 0;
    }

    result = ghostty_render_state_new(nullptr, &terminal->render_state);
    if (result != GHOSTTY_SUCCESS) {
        LOGE("ghostty_render_state_new failed: %d", result);
        ghostty_terminal_free(terminal->terminal);
        delete terminal;
        return 0;
    }

    result = createTerminalFormatter(
        terminal,
        GHOSTTY_FORMATTER_FORMAT_PLAIN,
        &terminal->plain_formatter
    );
    if (result != GHOSTTY_SUCCESS) {
        LOGE("plain formatter init failed: %d", result);
        ghostty_render_state_free(terminal->render_state);
        ghostty_terminal_free(terminal->terminal);
        delete terminal;
        return 0;
    }

    result = createTerminalFormatter(
        terminal,
        GHOSTTY_FORMATTER_FORMAT_HTML,
        &terminal->html_formatter
    );
    if (result != GHOSTTY_SUCCESS) {
        LOGE("html formatter init failed: %d", result);
        ghostty_formatter_free(terminal->plain_formatter);
        ghostty_render_state_free(terminal->render_state);
        ghostty_terminal_free(terminal->terminal);
        delete terminal;
        return 0;
    }

    result = ghostty_mouse_encoder_new(nullptr, &terminal->mouse_encoder);
    if (result != GHOSTTY_SUCCESS) {
        LOGE("mouse encoder init failed: %d", result);
        ghostty_formatter_free(terminal->html_formatter);
        ghostty_formatter_free(terminal->plain_formatter);
        ghostty_render_state_free(terminal->render_state);
        ghostty_terminal_free(terminal->terminal);
        delete terminal;
        return 0;
    }

    const bool trackLastCell = true;
    ghostty_mouse_encoder_setopt(
        terminal->mouse_encoder,
        GHOSTTY_MOUSE_ENCODER_OPT_TRACK_LAST_CELL,
        &trackLastCell
    );

    LOGI("Terminal created: %ldx%ld", static_cast<long>(cols), static_cast<long>(rows));
    return static_cast<unsigned long long>(reinterpret_cast<uintptr_t>(terminal));
}

+ (void)destroyTerminal:(unsigned long long)handle {
    auto *terminal = TerminalFromHandle(handle);
    if (!terminal) {
        return;
    }

    ghostty_mouse_encoder_free(terminal->mouse_encoder);
    ghostty_formatter_free(terminal->html_formatter);
    ghostty_formatter_free(terminal->plain_formatter);
    ghostty_render_state_free(terminal->render_state);
    ghostty_terminal_free(terminal->terminal);
    delete terminal;
    LOGI("Terminal destroyed");
}

+ (void)writeData:(NSString *)data toTerminal:(unsigned long long)handle {
    auto *terminal = TerminalFromHandle(handle);
    if (!terminal || data.length == 0) {
        return;
    }

    NSData *encoded = [data dataUsingEncoding:NSUTF8StringEncoding allowLossyConversion:YES];
    if (!encoded) {
        return;
    }

    ghostty_terminal_vt_write(
        terminal->terminal,
        static_cast<const uint8_t *>(encoded.bytes),
        encoded.length
    );
}

+ (void)scrollTerminal:(unsigned long long)handle byLines:(NSInteger)delta {
    auto *terminal = TerminalFromHandle(handle);
    if (!terminal || delta == 0) {
        return;
    }

    GhosttyTerminalScrollViewport behavior = {};
    behavior.tag = GHOSTTY_SCROLL_VIEWPORT_DELTA;
    behavior.value.delta = static_cast<intptr_t>(delta);
    ghostty_terminal_scroll_viewport(terminal->terminal, behavior);
    markFullSnapshot(terminal);
}

+ (void)scrollTerminalToBottom:(unsigned long long)handle {
    auto *terminal = TerminalFromHandle(handle);
    if (!terminal) {
        return;
    }

    GhosttyTerminalScrollViewport behavior = {};
    behavior.tag = GHOSTTY_SCROLL_VIEWPORT_BOTTOM;
    ghostty_terminal_scroll_viewport(terminal->terminal, behavior);
    markFullSnapshot(terminal);
}

+ (void)resizeTerminal:(unsigned long long)handle
                   cols:(NSInteger)cols
                   rows:(NSInteger)rows
              cellWidth:(double)cellWidth
             cellHeight:(double)cellHeight {
    auto *terminal = TerminalFromHandle(handle);
    if (!terminal || cols <= 0 || rows <= 0 || cols > UINT16_MAX || rows > UINT16_MAX) {
        return;
    }

    terminal->cols = static_cast<uint16_t>(cols);
    terminal->rows = static_cast<uint16_t>(rows);
    terminal->cell_width_px = roundPositivePixels(static_cast<float>(cellWidth));
    terminal->cell_height_px = roundPositivePixels(static_cast<float>(cellHeight));

    ghostty_terminal_resize(
        terminal->terminal,
        terminal->cols,
        terminal->rows,
        terminal->cell_width_px,
        terminal->cell_height_px
    );
    markFullSnapshot(terminal);
}

+ (BOOL)setThemeForTerminal:(unsigned long long)handle
                  foreground:(NSString *)foreground
                  background:(NSString *)background
                      cursor:(NSString *)cursor
                     palette:(NSArray<NSString *> *)palette {
    auto *terminal = TerminalFromHandle(handle);
    if (!terminal || palette.count < 256) {
        return NO;
    }

    GhosttyColorRgb foregroundColor = {};
    GhosttyColorRgb backgroundColor = {};
    GhosttyColorRgb cursorColor = {};
    if (!parseHexColor(foreground.UTF8String, &foregroundColor) ||
        !parseHexColor(background.UTF8String, &backgroundColor) ||
        !parseHexColor(cursor.UTF8String, &cursorColor)) {
        LOGE("setTheme received an invalid theme color");
        return NO;
    }

    std::array<GhosttyColorRgb, 256> paletteColors = {};
    for (NSUInteger index = 0; index < 256; index += 1) {
        NSString *entry = palette[index];
        if (![entry isKindOfClass:NSString.class] ||
            !parseHexColor(entry.UTF8String, &paletteColors[index])) {
            LOGE("setTheme received an invalid palette color at index %lu", static_cast<unsigned long>(index));
            return NO;
        }
    }

    if (!setTerminalOption(terminal, GHOSTTY_TERMINAL_OPT_COLOR_FOREGROUND, &foregroundColor, "foreground") ||
        !setTerminalOption(terminal, GHOSTTY_TERMINAL_OPT_COLOR_BACKGROUND, &backgroundColor, "background") ||
        !setTerminalOption(terminal, GHOSTTY_TERMINAL_OPT_COLOR_CURSOR, &cursorColor, "cursor") ||
        !setTerminalOption(terminal, GHOSTTY_TERMINAL_OPT_COLOR_PALETTE, paletteColors.data(), "palette")) {
        return NO;
    }

    markFullSnapshot(terminal);
    return YES;
}

+ (NSString *)encodeMouseForTerminal:(unsigned long long)handle
                               action:(NSInteger)action
                               button:(NSInteger)button
                                    x:(double)x
                                    y:(double)y
                                 mods:(NSInteger)mods
                     anyButtonPressed:(BOOL)anyButtonPressed {
    auto *terminal = TerminalFromHandle(handle);
    if (!terminal) {
        return @"";
    }

    GhosttyMouseAction decodedAction = GHOSTTY_MOUSE_ACTION_PRESS;
    GhosttyMouseButton decodedButton = GHOSTTY_MOUSE_BUTTON_UNKNOWN;
    bool hasButton = false;
    if (!decodeMouseAction(static_cast<int>(action), &decodedAction) ||
        !decodeMouseButton(static_cast<int>(button), &decodedButton, &hasButton)) {
        return @"";
    }

    const std::string encoded = encodeMouseSequence(
        terminal,
        decodedAction,
        decodedButton,
        hasButton,
        static_cast<float>(x),
        static_cast<float>(y),
        static_cast<GhosttyMods>(mods),
        anyButtonPressed
    );
    return NSStringFromStdString(encoded);
}

+ (NSDictionary<NSString *, id> *)renderSnapshotForTerminal:(unsigned long long)handle {
    auto *terminal = TerminalFromHandle(handle);
    if (!terminal) {
        return @{ @"dirty": @"none" };
    }

    if (ghostty_render_state_update(terminal->render_state, terminal->terminal) != GHOSTTY_SUCCESS) {
        return @{ @"dirty": @"none" };
    }

    uint16_t renderRows = terminal->rows;
    uint16_t renderCols = terminal->cols;
    ghostty_render_state_get(terminal->render_state, GHOSTTY_RENDER_STATE_DATA_ROWS, &renderRows);
    ghostty_render_state_get(terminal->render_state, GHOSTTY_RENDER_STATE_DATA_COLS, &renderCols);

    GhosttyRenderStateDirty dirty = GHOSTTY_RENDER_STATE_DIRTY_FALSE;
    ghostty_render_state_get(terminal->render_state, GHOSTTY_RENDER_STATE_DATA_DIRTY, &dirty);
    if (terminal->force_full_snapshot) {
        dirty = GHOSTTY_RENDER_STATE_DIRTY_FULL;
    }

    NSString *dirtyValue = @"none";
    if (dirty == GHOSTTY_RENDER_STATE_DIRTY_PARTIAL) {
        dirtyValue = @"partial";
    } else if (dirty == GHOSTTY_RENDER_STATE_DIRTY_FULL) {
        dirtyValue = @"full";
    }

    NSMutableDictionary<NSString *, id> *snapshot = [@{
        @"dirty": dirtyValue,
        @"rows": @(renderRows),
        @"cols": @(renderCols),
    } mutableCopy];

    if (dirty == GHOSTTY_RENDER_STATE_DIRTY_FALSE) {
        return snapshot;
    }

    bool cursorInViewport = false;
    ghostty_render_state_get(
        terminal->render_state,
        GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_HAS_VALUE,
        &cursorInViewport
    );

    if (cursorInViewport) {
        uint16_t cursorX = 0;
        uint16_t cursorY = 0;
        bool cursorVisible = false;
        ghostty_render_state_get(terminal->render_state, GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_X, &cursorX);
        ghostty_render_state_get(terminal->render_state, GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_Y, &cursorY);
        ghostty_render_state_get(terminal->render_state, GHOSTTY_RENDER_STATE_DATA_CURSOR_VISIBLE, &cursorVisible);
        snapshot[@"cursorCol"] = @(cursorX);
        snapshot[@"cursorRow"] = @(cursorY);
        snapshot[@"cursorVisible"] = @(cursorVisible);
    } else {
        snapshot[@"cursorCol"] = @0;
        snapshot[@"cursorRow"] = @0;
        snapshot[@"cursorVisible"] = @NO;
    }

    std::string visibleHTML;
    if (!buildVisibleHtml(terminal->render_state, renderRows, &visibleHTML)) {
        visibleHTML = formatTerminalScreen(terminal, GHOSTTY_FORMATTER_FORMAT_HTML);
    }
    snapshot[@"html"] = NSStringFromStdString(visibleHTML);

    terminal->force_full_snapshot = false;
    clearRenderStateDirty(terminal->render_state);
    return snapshot;
}

+ (NSString *)visibleTextForTerminal:(unsigned long long)handle {
    auto *terminal = TerminalFromHandle(handle);
    return NSStringFromStdString(formatTerminalScreen(terminal, GHOSTTY_FORMATTER_FORMAT_PLAIN));
}

+ (NSString *)visibleHTMLForTerminal:(unsigned long long)handle {
    auto *terminal = TerminalFromHandle(handle);
    if (!terminal) {
        return @"";
    }

    if (ghostty_render_state_update(terminal->render_state, terminal->terminal) != GHOSTTY_SUCCESS) {
        return NSStringFromStdString(formatTerminalScreen(terminal, GHOSTTY_FORMATTER_FORMAT_HTML));
    }

    uint16_t renderRows = terminal->rows;
    ghostty_render_state_get(terminal->render_state, GHOSTTY_RENDER_STATE_DATA_ROWS, &renderRows);

    std::string visibleHTML;
    if (!buildVisibleHtml(terminal->render_state, renderRows, &visibleHTML)) {
        visibleHTML = formatTerminalScreen(terminal, GHOSTTY_FORMATTER_FORMAT_HTML);
    }
    return NSStringFromStdString(visibleHTML);
}

@end
