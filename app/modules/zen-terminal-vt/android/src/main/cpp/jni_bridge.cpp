#include <jni.h>
#include <android/log.h>
#include <cstring>
#include <cstdlib>
#include <vector>
#include <ghostty/vt.h>

#define TAG "ZenTerminalVt"
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, TAG, __VA_ARGS__)
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, TAG, __VA_ARGS__)

// Per-cell packed data: [codepoint, fgARGB, bgARGB, flags]
#define INTS_PER_CELL 4

// Style flags bitmask (matches src/index.ts CellFlags)
#define FLAG_BOLD          (1 << 0)
#define FLAG_ITALIC        (1 << 1)
#define FLAG_UNDERLINE     (1 << 2)
#define FLAG_STRIKETHROUGH (1 << 3)
#define FLAG_INVERSE       (1 << 4)
// bit 5 reserved for WIDE (from cell width > 1)

static inline jint packRgb(GhosttyColorRgb c) {
    return (jint)(0xFF000000u | ((uint32_t)c.r << 16) | ((uint32_t)c.g << 8) | c.b);
}

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
 * Per-terminal state: owns the terminal, render state, and iterators.
 */
struct TerminalHandle {
    GhosttyTerminal terminal = nullptr;
    GhosttyRenderState render_state = nullptr;
    GhosttyRenderStateRowIterator row_iter = nullptr;
    GhosttyRenderStateRowCells row_cells = nullptr;
    GhosttyFormatter formatter = nullptr;
    uint16_t cols = 0;
    uint16_t rows = 0;
};

static jstring formatTerminalScreen(
    JNIEnv* env,
    TerminalHandle* h,
    GhosttyFormatterFormat format)
{
    if (!h) return env->NewStringUTF("");

    GhosttyFormatter fmt = nullptr;
    GhosttyFormatterTerminalOptions opts = GHOSTTY_INIT_SIZED(GhosttyFormatterTerminalOptions);
    opts.emit = format;
    opts.trim = true;

    GhosttyResult res = ghostty_formatter_terminal_new(nullptr, &fmt, h->terminal, opts);
    if (res != GHOSTTY_SUCCESS) {
        LOGE("ghostty_formatter_terminal_new failed: %d", res);
        return env->NewStringUTF("");
    }

    uint8_t* outPtr = nullptr;
    size_t outLen = 0;
    res = ghostty_formatter_format_alloc(fmt, nullptr, &outPtr, &outLen);
    ghostty_formatter_free(fmt);

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
    opts.max_scrollback = 0; // tmux holds history

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

    res = ghostty_render_state_row_iterator_new(nullptr, &h->row_iter);
    if (res != GHOSTTY_SUCCESS) {
        LOGE("ghostty_render_state_row_iterator_new failed: %d", res);
        ghostty_render_state_free(h->render_state);
        ghostty_terminal_free(h->terminal);
        delete h;
        return 0;
    }

    res = ghostty_render_state_row_cells_new(nullptr, &h->row_cells);
    if (res != GHOSTTY_SUCCESS) {
        LOGE("ghostty_render_state_row_cells_new failed: %d", res);
        ghostty_render_state_row_iterator_free(h->row_iter);
        ghostty_render_state_free(h->render_state);
        ghostty_terminal_free(h->terminal);
        delete h;
        return 0;
    }

    LOGI("Terminal created: %dx%d", cols, rows);
    return reinterpret_cast<jlong>(h);
}

JNIEXPORT void JNICALL
Java_expo_modules_zenterminalvt_ZenTerminalVtModule_nativeDestroyTerminal(
    JNIEnv*, jobject, jlong handle)
{
    auto* h = getHandle(handle);
    if (!h) return;

    if (h->formatter) ghostty_formatter_free(h->formatter);
    ghostty_render_state_row_cells_free(h->row_cells);
    ghostty_render_state_row_iterator_free(h->row_iter);
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
Java_expo_modules_zenterminalvt_ZenTerminalVtModule_nativeResize(
    JNIEnv*, jobject, jlong handle,
    jint cols, jint rows, jfloat cellWidth, jfloat cellHeight)
{
    auto* h = getHandle(handle);
    if (!h) return;

    h->cols = (uint16_t)cols;
    h->rows = (uint16_t)rows;

    ghostty_terminal_resize(
        h->terminal,
        h->cols, h->rows,
        (uint32_t)cellWidth, (uint32_t)cellHeight
    );
}

JNIEXPORT jobject JNICALL
Java_expo_modules_zenterminalvt_ZenTerminalVtModule_nativeGetRenderState(
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

    // Update render state from terminal
    ghostty_render_state_update(h->render_state, h->terminal);

    // Check dirty state
    GhosttyRenderStateDirty dirty;
    ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_DIRTY, &dirty);

    const char* dirtyStr;
    switch (dirty) {
        case GHOSTTY_RENDER_STATE_DIRTY_PARTIAL: dirtyStr = "partial"; break;
        case GHOSTTY_RENDER_STATE_DIRTY_FULL:    dirtyStr = "full"; break;
        default:                                  dirtyStr = "none"; break;
    }
    uint16_t renderRows = h->rows;
    uint16_t renderCols = h->cols;
    ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_ROWS, &renderRows);
    ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_COLS, &renderCols);

    putStr("dirty", dirtyStr);
    putInt("rows", (jint)renderRows);
    putInt("cols", (jint)renderCols);

    if (dirty == GHOSTTY_RENDER_STATE_DIRTY_FALSE) {
        return map;
    }

    // Get default colors
    GhosttyColorRgb bgDefault, fgDefault;
    ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_COLOR_BACKGROUND, &bgDefault);
    ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_COLOR_FOREGROUND, &fgDefault);
    jint defaultFg = packRgb(fgDefault);

    // Populate row iterator from render state
    ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, h->row_iter);

    // Iterate rows and cells, pack into flat int array
    size_t totalCells = (size_t)renderCols * renderRows;
    jintArray cells = env->NewIntArray((jsize)(totalCells * INTS_PER_CELL));
    jint* buf = env->GetIntArrayElements(cells, nullptr);

    size_t cellIdx = 0;
    uint16_t rowCount = 0;
    std::vector<uint32_t> graphemeHeap;
    while (rowCount < renderRows && ghostty_render_state_row_iterator_next(h->row_iter)) {
        // Get cells for this row
        if (ghostty_render_state_row_get(
                h->row_iter,
                GHOSTTY_RENDER_STATE_ROW_DATA_CELLS,
                h->row_cells) != GHOSTTY_SUCCESS) {
            rowCount++;
            continue;
        }

        uint16_t colCount = 0;
        while (colCount < renderCols && ghostty_render_state_row_cells_next(h->row_cells)) {
            if (cellIdx >= totalCells) {
                break;
            }
            size_t off = cellIdx * INTS_PER_CELL;

            // Codepoint: get grapheme length, then first codepoint
            uint32_t graphLen = 0;
            ghostty_render_state_row_cells_get(h->row_cells,
                GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_LEN, &graphLen);

            uint32_t codepoint = 0;
            if (graphLen > 0) {
                uint32_t graphemeStack[8];
                uint32_t* graphemes = graphemeStack;
                if (graphLen > (sizeof(graphemeStack) / sizeof(graphemeStack[0]))) {
                    graphemeHeap.resize(graphLen);
                    graphemes = graphemeHeap.data();
                }
                if (ghostty_render_state_row_cells_get(
                        h->row_cells,
                        GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_BUF,
                        graphemes) == GHOSTTY_SUCCESS) {
                    codepoint = graphemes[0];
                }
            }
            buf[off + 0] = (jint)codepoint;

            // Foreground color
            GhosttyColorRgb fg;
            GhosttyResult fgRes = ghostty_render_state_row_cells_get(h->row_cells,
                GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_FG_COLOR, &fg);
            buf[off + 1] = (fgRes == GHOSTTY_SUCCESS) ? packRgb(fg) : defaultFg;

            // Background color
            GhosttyColorRgb bg;
            GhosttyResult bgRes = ghostty_render_state_row_cells_get(h->row_cells,
                GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_BG_COLOR, &bg);
            buf[off + 2] = (bgRes == GHOSTTY_SUCCESS) ? packRgb(bg) : 0;

            // Style flags
            GhosttyStyle style = GHOSTTY_INIT_SIZED(GhosttyStyle);
            ghostty_render_state_row_cells_get(h->row_cells,
                GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE, &style);

            jint flags = 0;
            if (style.bold)          flags |= FLAG_BOLD;
            if (style.italic)        flags |= FLAG_ITALIC;
            if (style.underline > 0) flags |= FLAG_UNDERLINE;
            if (style.strikethrough) flags |= FLAG_STRIKETHROUGH;
            if (style.inverse)       flags |= FLAG_INVERSE;
            buf[off + 3] = flags;

            cellIdx++;
            colCount++;
        }
        rowCount++;
    }

    env->ReleaseIntArrayElements(cells, buf, 0);
    env->CallObjectMethod(map, mapPut, env->NewStringUTF("cells"), cells);

    // Reset dirty state after reading
    GhosttyRenderStateDirty resetDirty = GHOSTTY_RENDER_STATE_DIRTY_FALSE;
    ghostty_render_state_set(h->render_state, GHOSTTY_RENDER_STATE_OPTION_DIRTY, &resetDirty);

    // Do not manually clear per-row dirty flags here.
    //
    // The libghostty row dirty reset path crashes on Android in
    // ghostty_render_state_row_set(). The surface only consumes full-frame
    // snapshots today, so clearing the top-level dirty bit is sufficient.

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
    return formatTerminalScreen(env, h, GHOSTTY_FORMATTER_FORMAT_HTML);
}

} // extern "C"
