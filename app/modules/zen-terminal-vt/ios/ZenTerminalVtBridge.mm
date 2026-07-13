#import "ZenTerminalVtBridge.h"

#include <array>
#include <algorithm>
#include <cmath>
#include <limits>
#include <memory>
#include <string>
#include <vector>
#include <ghostty/vt.h>

namespace {
constexpr size_t kDefaultScrollbackRows = 10000;
constexpr size_t kMouseStackBufferSize = 128;

struct TerminalState {
  GhosttyTerminal terminal = nullptr;
  GhosttyRenderState renderState = nullptr;
  GhosttyFormatter plainFormatter = nullptr;
  GhosttyFormatter htmlFormatter = nullptr;
  GhosttyMouseEncoder mouseEncoder = nullptr;
  uint16_t columns = 0;
  uint16_t rows = 0;
  uint32_t cellWidth = 1;
  uint32_t cellHeight = 1;
  bool forceFullSnapshot = true;

  ~TerminalState() {
    if (mouseEncoder) ghostty_mouse_encoder_free(mouseEncoder);
    if (htmlFormatter) ghostty_formatter_free(htmlFormatter);
    if (plainFormatter) ghostty_formatter_free(plainFormatter);
    if (renderState) ghostty_render_state_free(renderState);
    if (terminal) ghostty_terminal_free(terminal);
  }
};

NSDictionary<NSString *, id> *success(id _Nullable value = nil) {
  return value ? @{ @"ok": @YES, @"value": value } : @{ @"ok": @YES };
}

NSDictionary<NSString *, id> *failure(NSString *message) {
  return @{ @"ok": @NO, @"error": message };
}

bool validGrid(NSInteger columns, NSInteger rows) {
  return columns > 0 && rows > 0 &&
    columns <= std::numeric_limits<uint16_t>::max() &&
    rows <= std::numeric_limits<uint16_t>::max();
}

uint32_t roundedPixels(double value) {
  if (!std::isfinite(value) || value <= 0) return 1;
  const double rounded = std::round(value);
  return rounded >= std::numeric_limits<uint32_t>::max()
    ? std::numeric_limits<uint32_t>::max()
    : static_cast<uint32_t>(rounded);
}

bool parseColor(NSString *value, GhosttyColorRgb *out) {
  if (!out || value.length != 7 || ![value hasPrefix:@"#"]) return false;
  unsigned int rgb = 0;
  NSScanner *scanner = [NSScanner scannerWithString:[value substringFromIndex:1]];
  if (![scanner scanHexInt:&rgb] || !scanner.isAtEnd) return false;
  out->r = static_cast<uint8_t>((rgb >> 16) & 0xff);
  out->g = static_cast<uint8_t>((rgb >> 8) & 0xff);
  out->b = static_cast<uint8_t>(rgb & 0xff);
  return true;
}

GhosttyResult makeFormatter(TerminalState *state, GhosttyFormatterFormat format,
                            GhosttyFormatter *out) {
  GhosttyFormatterTerminalOptions options = GHOSTTY_INIT_SIZED(GhosttyFormatterTerminalOptions);
  options.emit = format;
  options.trim = format == GHOSTTY_FORMATTER_FORMAT_PLAIN;
  return ghostty_formatter_terminal_new(nullptr, out, state->terminal, options);
}

NSString *format(TerminalState *state, GhosttyFormatter formatter) {
  if (!formatter) return @"";
  uint8_t *bytes = nullptr;
  size_t length = 0;
  if (ghostty_formatter_format_alloc(formatter, nullptr, &bytes, &length) != GHOSTTY_SUCCESS || !bytes) {
    return @"";
  }
  NSString *value = [[NSString alloc] initWithBytes:bytes length:length encoding:NSUTF8StringEncoding];
  ghostty_free(nullptr, bytes, length);
  return value ?: @"";
}

bool decodeAction(NSInteger value, GhosttyMouseAction *out) {
  switch (value) {
    case 0: *out = GHOSTTY_MOUSE_ACTION_PRESS; return true;
    case 1: *out = GHOSTTY_MOUSE_ACTION_RELEASE; return true;
    case 2: *out = GHOSTTY_MOUSE_ACTION_MOTION; return true;
    default: return false;
  }
}

bool decodeButton(NSInteger value, GhosttyMouseButton *out, bool *hasButton) {
  *hasButton = true;
  switch (value) {
    case 0: *hasButton = false; *out = GHOSTTY_MOUSE_BUTTON_UNKNOWN; return true;
    case 1: *out = GHOSTTY_MOUSE_BUTTON_LEFT; return true;
    case 2: *out = GHOSTTY_MOUSE_BUTTON_RIGHT; return true;
    case 3: *out = GHOSTTY_MOUSE_BUTTON_MIDDLE; return true;
    // JS wheelUp/wheelDown intentionally map to the pinned ABI's buttons 4/5,
    // matching Android's JNI bridge.
    case 4: *out = GHOSTTY_MOUSE_BUTTON_FOUR; return true;
    case 5: *out = GHOSTTY_MOUSE_BUTTON_FIVE; return true;
    default: return false;
  }
}

void clearDirtyState(GhosttyRenderState renderState) {
  GhosttyRenderStateDirty clean = GHOSTTY_RENDER_STATE_DIRTY_FALSE;
  ghostty_render_state_set(renderState, GHOSTTY_RENDER_STATE_OPTION_DIRTY, &clean);
  GhosttyRenderStateRowIterator iterator = nullptr;
  if (ghostty_render_state_row_iterator_new(nullptr, &iterator) != GHOSTTY_SUCCESS || !iterator) return;
  if (ghostty_render_state_get(renderState, GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, &iterator) != GHOSTTY_SUCCESS) {
    ghostty_render_state_row_iterator_free(iterator);
    return;
  }
  const bool rowClean = false;
  while (ghostty_render_state_row_iterator_next(iterator)) {
    ghostty_render_state_row_set(iterator, GHOSTTY_RENDER_STATE_ROW_OPTION_DIRTY, &rowClean);
  }
  ghostty_render_state_row_iterator_free(iterator);
}
}  // namespace

@implementation ZenTerminalVtBridge {
  NSRecursiveLock *_lock;
  NSMutableDictionary<NSNumber *, NSValue *> *_terminals;
  NSInteger _nextHandle;
}

- (instancetype)init {
  if ((self = [super init])) {
    _lock = [[NSRecursiveLock alloc] init];
    _terminals = [[NSMutableDictionary alloc] init];
    _nextHandle = 1;
  }
  return self;
}

- (void)dealloc {
  [_lock lock];
  for (NSValue *value in _terminals.allValues) {
    delete static_cast<TerminalState *>(value.pointerValue);
  }
  [_terminals removeAllObjects];
  [_lock unlock];
}

- (TerminalState *)stateForHandle:(NSInteger)handle {
  return static_cast<TerminalState *>(_terminals[@(handle)].pointerValue);
}

- (NSDictionary<NSString *, id> *)unknownHandle:(NSInteger)handle {
  return failure([NSString stringWithFormat:@"Unknown terminal handle id: %ld", (long)handle]);
}

- (NSDictionary<NSString *, id> *)createTerminalWithColumns:(NSInteger)columns rows:(NSInteger)rows {
  [_lock lock];
  @try {
    if (!validGrid(columns, rows)) return failure(@"Terminal columns and rows must be in 1...65535");
    auto state = std::make_unique<TerminalState>();
    state->columns = static_cast<uint16_t>(columns);
    state->rows = static_cast<uint16_t>(rows);
    GhosttyTerminalOptions options = {};
    options.cols = state->columns;
    options.rows = state->rows;
    options.max_scrollback = kDefaultScrollbackRows;
    if (ghostty_terminal_new(nullptr, &state->terminal, options) != GHOSTTY_SUCCESS ||
        ghostty_render_state_new(nullptr, &state->renderState) != GHOSTTY_SUCCESS ||
        makeFormatter(state.get(), GHOSTTY_FORMATTER_FORMAT_PLAIN, &state->plainFormatter) != GHOSTTY_SUCCESS ||
        makeFormatter(state.get(), GHOSTTY_FORMATTER_FORMAT_HTML, &state->htmlFormatter) != GHOSTTY_SUCCESS ||
        ghostty_mouse_encoder_new(nullptr, &state->mouseEncoder) != GHOSTTY_SUCCESS) {
      return failure(@"libghostty-vt failed to allocate terminal state");
    }
    const bool trackLastCell = true;
    ghostty_mouse_encoder_setopt(state->mouseEncoder, GHOSTTY_MOUSE_ENCODER_OPT_TRACK_LAST_CELL, &trackLastCell);
    NSInteger handle = 0;
    do {
      handle = _nextHandle;
      _nextHandle = _nextHandle == NSIntegerMax ? 1 : _nextHandle + 1;
    } while (_terminals[@(handle)] != nil);
    _terminals[@(handle)] = [NSValue valueWithPointer:state.release()];
    return success(@(handle));
  } @finally {
    [_lock unlock];
  }
}

- (void)destroyTerminal:(NSInteger)handle {
  [_lock lock];
  NSValue *owned = _terminals[@(handle)];
  if (owned) {
    [_terminals removeObjectForKey:@(handle)];
    delete static_cast<TerminalState *>(owned.pointerValue);
  }
  [_lock unlock];
}

- (NSDictionary<NSString *, id> *)writeData:(NSString *)data handle:(NSInteger)handle {
  [_lock lock];
  TerminalState *state = [self stateForHandle:handle];
  NSDictionary *result = nil;
  if (!state) result = [self unknownHandle:handle];
  else {
    NSData *bytes = [data dataUsingEncoding:NSUTF8StringEncoding];
    ghostty_terminal_vt_write(state->terminal, static_cast<const uint8_t *>(bytes.bytes), bytes.length);
    result = success();
  }
  [_lock unlock];
  return result;
}

- (NSDictionary<NSString *, id> *)scrollViewport:(NSInteger)delta handle:(NSInteger)handle {
  [_lock lock];
  TerminalState *state = [self stateForHandle:handle];
  if (!state) { [_lock unlock]; return [self unknownHandle:handle]; }
  if (delta != 0) {
    GhosttyTerminalScrollViewport behavior = {};
    behavior.tag = GHOSTTY_SCROLL_VIEWPORT_DELTA;
    behavior.value.delta = static_cast<intptr_t>(delta);
    ghostty_terminal_scroll_viewport(state->terminal, behavior);
    state->forceFullSnapshot = true;
  }
  [_lock unlock];
  return success();
}

- (NSDictionary<NSString *, id> *)scrollViewportToBottom:(NSInteger)handle {
  [_lock lock];
  TerminalState *state = [self stateForHandle:handle];
  if (!state) { [_lock unlock]; return [self unknownHandle:handle]; }
  GhosttyTerminalScrollViewport behavior = {};
  behavior.tag = GHOSTTY_SCROLL_VIEWPORT_BOTTOM;
  ghostty_terminal_scroll_viewport(state->terminal, behavior);
  state->forceFullSnapshot = true;
  [_lock unlock];
  return success();
}

- (NSDictionary<NSString *, id> *)resizeTerminal:(NSInteger)handle columns:(NSInteger)columns
                                             rows:(NSInteger)rows cellWidth:(double)cellWidth
                                       cellHeight:(double)cellHeight {
  [_lock lock];
  TerminalState *state = [self stateForHandle:handle];
  if (!state) { [_lock unlock]; return [self unknownHandle:handle]; }
  if (!validGrid(columns, rows)) { [_lock unlock]; return failure(@"Terminal columns and rows must be in 1...65535"); }
  state->columns = static_cast<uint16_t>(columns);
  state->rows = static_cast<uint16_t>(rows);
  state->cellWidth = roundedPixels(cellWidth);
  state->cellHeight = roundedPixels(cellHeight);
  ghostty_terminal_resize(state->terminal, state->columns, state->rows, state->cellWidth, state->cellHeight);
  state->forceFullSnapshot = true;
  [_lock unlock];
  return success();
}

- (NSDictionary<NSString *, id> *)setThemeForTerminal:(NSInteger)handle foreground:(NSString *)foreground
                                           background:(NSString *)background cursor:(NSString *)cursor
                                              palette:(NSArray<NSString *> *)palette {
  [_lock lock];
  TerminalState *state = [self stateForHandle:handle];
  if (!state) { [_lock unlock]; return [self unknownHandle:handle]; }
  GhosttyColorRgb fg = {}, bg = {}, cursorColor = {};
  if (palette.count < 256 || !parseColor(foreground, &fg) || !parseColor(background, &bg) || !parseColor(cursor, &cursorColor)) {
    [_lock unlock]; return failure(@"Theme requires #RRGGBB colors and a 256-entry palette");
  }
  std::array<GhosttyColorRgb, 256> colors = {};
  for (NSUInteger index = 0; index < colors.size(); index++) {
    if (!parseColor(palette[index], &colors[index])) { [_lock unlock]; return failure(@"Theme palette contains an invalid color"); }
  }
  const bool ok =
    ghostty_terminal_set(state->terminal, GHOSTTY_TERMINAL_OPT_COLOR_FOREGROUND, &fg) == GHOSTTY_SUCCESS &&
    ghostty_terminal_set(state->terminal, GHOSTTY_TERMINAL_OPT_COLOR_BACKGROUND, &bg) == GHOSTTY_SUCCESS &&
    ghostty_terminal_set(state->terminal, GHOSTTY_TERMINAL_OPT_COLOR_CURSOR, &cursorColor) == GHOSTTY_SUCCESS &&
    ghostty_terminal_set(state->terminal, GHOSTTY_TERMINAL_OPT_COLOR_PALETTE, colors.data()) == GHOSTTY_SUCCESS;
  state->forceFullSnapshot = ok;
  [_lock unlock];
  return ok ? success() : failure(@"libghostty-vt rejected the terminal theme");
}

- (NSDictionary<NSString *, id> *)encodeMouseEventForTerminal:(NSInteger)handle action:(NSInteger)action
                                                        button:(NSInteger)button x:(double)x y:(double)y
                                                          mods:(NSInteger)mods anyButtonPressed:(BOOL)anyButtonPressed {
  [_lock lock];
  TerminalState *state = [self stateForHandle:handle];
  if (!state) { [_lock unlock]; return [self unknownHandle:handle]; }
  GhosttyMouseAction decodedAction;
  GhosttyMouseButton decodedButton;
  bool hasButton;
  if (!decodeAction(action, &decodedAction) || !decodeButton(button, &decodedButton, &hasButton) || !std::isfinite(x) || !std::isfinite(y)) {
    [_lock unlock]; return failure(@"Invalid mouse event");
  }
  GhosttyMouseEncoderSize size = GHOSTTY_INIT_SIZED(GhosttyMouseEncoderSize);
  const uint64_t screenWidth = static_cast<uint64_t>(state->columns) * state->cellWidth;
  const uint64_t screenHeight = static_cast<uint64_t>(state->rows) * state->cellHeight;
  size.screen_width = static_cast<uint32_t>(std::min<uint64_t>(screenWidth, std::numeric_limits<uint32_t>::max()));
  size.screen_height = static_cast<uint32_t>(std::min<uint64_t>(screenHeight, std::numeric_limits<uint32_t>::max()));
  size.cell_width = state->cellWidth;
  size.cell_height = state->cellHeight;
  ghostty_mouse_encoder_setopt_from_terminal(state->mouseEncoder, state->terminal);
  ghostty_mouse_encoder_setopt(state->mouseEncoder, GHOSTTY_MOUSE_ENCODER_OPT_SIZE, &size);
  ghostty_mouse_encoder_setopt(state->mouseEncoder, GHOSTTY_MOUSE_ENCODER_OPT_ANY_BUTTON_PRESSED, &anyButtonPressed);
  if (decodedAction != GHOSTTY_MOUSE_ACTION_MOTION) ghostty_mouse_encoder_reset(state->mouseEncoder);
  GhosttyMouseEvent event = nullptr;
  GhosttyResult code = ghostty_mouse_event_new(nullptr, &event);
  std::array<char, kMouseStackBufferSize> bytes = {};
  size_t length = 0;
  if (code == GHOSTTY_SUCCESS) {
    ghostty_mouse_event_set_action(event, decodedAction);
    if (hasButton) ghostty_mouse_event_set_button(event, decodedButton); else ghostty_mouse_event_clear_button(event);
    ghostty_mouse_event_set_mods(event, static_cast<GhosttyMods>(mods));
    ghostty_mouse_event_set_position(event, GhosttyMousePosition{
      static_cast<float>(x), static_cast<float>(y)
    });
    code = ghostty_mouse_encoder_encode(state->mouseEncoder, event, bytes.data(), bytes.size(), &length);
    if (code == GHOSTTY_OUT_OF_SPACE && length > bytes.size()) {
      std::vector<char> dynamicBytes(length);
      code = ghostty_mouse_encoder_encode(
        state->mouseEncoder, event, dynamicBytes.data(), dynamicBytes.size(), &length
      );
      NSString *encoded = code == GHOSTTY_SUCCESS
        ? [[NSString alloc] initWithBytes:dynamicBytes.data() length:length encoding:NSUTF8StringEncoding] ?: @""
        : @"";
      ghostty_mouse_event_free(event);
      [_lock unlock];
      return success(encoded);
    }
    ghostty_mouse_event_free(event);
  }
  NSString *encoded = code == GHOSTTY_SUCCESS
    ? [[NSString alloc] initWithBytes:bytes.data() length:length encoding:NSUTF8StringEncoding] ?: @""
    : @"";
  [_lock unlock];
  return success(encoded);
}

- (NSDictionary<NSString *, id> *)renderSnapshotForTerminal:(NSInteger)handle {
  [_lock lock];
  TerminalState *state = [self stateForHandle:handle];
  if (!state) { [_lock unlock]; return [self unknownHandle:handle]; }
  if (ghostty_render_state_update(state->renderState, state->terminal) != GHOSTTY_SUCCESS) {
    [_lock unlock]; return failure(@"libghostty-vt failed to update render state");
  }
  uint16_t rows = state->rows, columns = state->columns;
  GhosttyRenderStateDirty dirty = GHOSTTY_RENDER_STATE_DIRTY_FALSE;
  ghostty_render_state_get(state->renderState, GHOSTTY_RENDER_STATE_DATA_ROWS, &rows);
  ghostty_render_state_get(state->renderState, GHOSTTY_RENDER_STATE_DATA_COLS, &columns);
  ghostty_render_state_get(state->renderState, GHOSTTY_RENDER_STATE_DATA_DIRTY, &dirty);
  if (state->forceFullSnapshot) dirty = GHOSTTY_RENDER_STATE_DIRTY_FULL;
  NSString *dirtyName = dirty == GHOSTTY_RENDER_STATE_DIRTY_FULL ? @"full" :
    dirty == GHOSTTY_RENDER_STATE_DIRTY_PARTIAL ? @"partial" : @"none";
  NSMutableDictionary *snapshot = [@{ @"dirty": dirtyName, @"rows": @(rows), @"cols": @(columns),
    @"cursorCol": @0, @"cursorRow": @0, @"cursorVisible": @NO } mutableCopy];
  if (dirty != GHOSTTY_RENDER_STATE_DIRTY_FALSE) {
    bool hasCursor = false, cursorVisible = false;
    uint16_t cursorX = 0, cursorY = 0;
    ghostty_render_state_get(state->renderState, GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_HAS_VALUE, &hasCursor);
    if (hasCursor) {
      ghostty_render_state_get(state->renderState, GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_X, &cursorX);
      ghostty_render_state_get(state->renderState, GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_Y, &cursorY);
      ghostty_render_state_get(state->renderState, GHOSTTY_RENDER_STATE_DATA_CURSOR_VISIBLE, &cursorVisible);
    }
    snapshot[@"cursorCol"] = @(cursorX);
    snapshot[@"cursorRow"] = @(cursorY);
    snapshot[@"cursorVisible"] = @(hasCursor && cursorVisible);
    snapshot[@"html"] = format(state, state->htmlFormatter);
    state->forceFullSnapshot = false;
    clearDirtyState(state->renderState);
  }
  [_lock unlock];
  return success(snapshot);
}

- (NSDictionary<NSString *, id> *)visibleTextForTerminal:(NSInteger)handle {
  [_lock lock]; TerminalState *state = [self stateForHandle:handle];
  NSDictionary *result = state ? success(format(state, state->plainFormatter)) : [self unknownHandle:handle];
  [_lock unlock]; return result;
}

- (NSDictionary<NSString *, id> *)visibleHTMLForTerminal:(NSInteger)handle {
  [_lock lock]; TerminalState *state = [self stateForHandle:handle];
  NSDictionary *result = state ? success(format(state, state->htmlFormatter)) : [self unknownHandle:handle];
  [_lock unlock]; return result;
}

@end
