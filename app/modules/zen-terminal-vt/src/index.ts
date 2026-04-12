import { requireNativeModule } from 'expo-modules-core';

type NativeRenderSnapshot = Omit<RenderSnapshot, 'html' | 'lineHtml' | 'dirtyLines'> & {
  html?: string;
  lineHtml?: string[];
  dirtyLines?: number[];
};

export type MouseAction = 'press' | 'release' | 'motion';
export type MouseButton = 'none' | 'left' | 'middle' | 'right' | 'wheelUp' | 'wheelDown';

export interface MouseEventPayload {
  action: MouseAction;
  button: MouseButton;
  x: number;
  y: number;
  shift?: boolean;
  ctrl?: boolean;
  alt?: boolean;
  meta?: boolean;
  anyButtonPressed?: boolean;
}

export interface NativeTerminalTheme {
  foreground: string;
  background: string;
  cursor: string;
  palette: readonly string[];
}

interface NativeTerminalVtModule {
  createTerminal(cols: number, rows: number): number;
  destroyTerminal(handle: number): void;
  writeData(handle: number, data: string): void;
  scrollViewport?(handle: number, delta: number): void;
  scrollViewportToBottom?(handle: number): void;
  resize(handle: number, cols: number, rows: number, cellWidth: number, cellHeight: number): void;
  setTheme?(handle: number, foreground: string, background: string, cursor: string, palette: string[]): void;
  encodeMouseEvent?(
    handle: number,
    action: number,
    button: number,
    x: number,
    y: number,
    mods: number,
    anyButtonPressed: boolean,
  ): string;
  getRenderSnapshot?: (handle: number) => NativeRenderSnapshot;
  getRenderState?: (handle: number) => NativeRenderSnapshot;
  getVisibleText(handle: number): string;
  getVisibleHtml(handle: number): string;
  getCrashBreadcrumb(): NativeCrashBreadcrumb | null;
  clearCrashBreadcrumb(): void;
}

const ZenTerminalVt = requireNativeModule<NativeTerminalVtModule>('ZenTerminalVt');

const MOUSE_ACTION_CODES: Record<MouseAction, number> = {
  press: 0,
  release: 1,
  motion: 2,
};

const MOUSE_BUTTON_CODES: Record<MouseButton, number> = {
  none: 0,
  left: 1,
  right: 2,
  middle: 3,
  wheelUp: 4,
  wheelDown: 5,
};

const MOD_SHIFT = 1 << 0;
const MOD_CTRL = 1 << 1;
const MOD_ALT = 1 << 2;
const MOD_SUPER = 1 << 3;

export interface RenderSnapshot {
  dirty: 'none' | 'partial' | 'full';
  rows: number;
  cols: number;
  html: string;
  lineHtml?: string[];
  dirtyLines?: number[];
  cursorCol: number;
  cursorRow: number;
  cursorVisible: boolean;
}

export interface NativeCrashBreadcrumb {
  stage: 'before' | 'after';
  operation: string;
  detail: string;
  timestampMs: number;
  abi: string;
  model: string;
  brand: string;
  sdkInt: number;
}

function readNativeSnapshot(handle: number): NativeRenderSnapshot | null {
  if (typeof ZenTerminalVt.getRenderSnapshot === 'function') {
    return ZenTerminalVt.getRenderSnapshot(handle);
  }

  if (typeof ZenTerminalVt.getRenderState === 'function') {
    return ZenTerminalVt.getRenderState(handle);
  }

  return null;
}

/**
 * Create a libghostty-vt terminal instance.
 * Returns an opaque handle (number) for subsequent calls.
 */
export function createTerminal(cols: number, rows: number): number {
  return ZenTerminalVt.createTerminal(cols, rows);
}

/**
 * Destroy a terminal instance and free native resources.
 */
export function destroyTerminal(handle: number): void {
  ZenTerminalVt.destroyTerminal(handle);
}

/**
 * Feed raw PTY output (UTF-8 string) into the terminal's VT parser.
 */
export function writeData(handle: number, data: string): void {
  ZenTerminalVt.writeData(handle, data);
}

/**
 * Scroll the local viewport by terminal rows. Negative = older content, positive = newer.
 */
export function scrollViewport(handle: number, delta: number): void {
  if (typeof ZenTerminalVt.scrollViewport !== 'function' || delta === 0) {
    return;
  }

  ZenTerminalVt.scrollViewport(handle, delta);
}

/**
 * Return the local viewport to the live bottom.
 */
export function scrollViewportToBottom(handle: number): void {
  if (typeof ZenTerminalVt.scrollViewportToBottom !== 'function') {
    return;
  }

  ZenTerminalVt.scrollViewportToBottom(handle);
}

/**
 * Resize the terminal grid.
 */
export function resize(
  handle: number,
  cols: number,
  rows: number,
  cellWidth: number,
  cellHeight: number,
): void {
  ZenTerminalVt.resize(handle, cols, rows, cellWidth, cellHeight);
}

/**
 * Apply the embedder theme so libghostty renders ANSI colors with the app palette.
 */
export function setTheme(handle: number, theme: NativeTerminalTheme): void {
  if (typeof ZenTerminalVt.setTheme !== 'function') {
    return;
  }

  ZenTerminalVt.setTheme(
    handle,
    theme.foreground,
    theme.background,
    theme.cursor,
    [...theme.palette],
  );
}

/**
 * Encode a mouse event using the terminal's current tracking mode.
 */
export function encodeMouseEvent(handle: number, event: MouseEventPayload): string {
  if (typeof ZenTerminalVt.encodeMouseEvent !== 'function') {
    return '';
  }

  let mods = 0;
  if (event.shift) {
    mods |= MOD_SHIFT;
  }
  if (event.ctrl) {
    mods |= MOD_CTRL;
  }
  if (event.alt) {
    mods |= MOD_ALT;
  }
  if (event.meta) {
    mods |= MOD_SUPER;
  }

  return ZenTerminalVt.encodeMouseEvent(
    handle,
    MOUSE_ACTION_CODES[event.action],
    MOUSE_BUTTON_CODES[event.button],
    event.x,
    event.y,
    mods,
    Boolean(event.anyButtonPressed),
  );
}

/**
 * Extract the current terminal snapshot for rendering.
 */
export function getRenderSnapshot(handle: number): RenderSnapshot {
  const snapshot = readNativeSnapshot(handle);

  if (!snapshot || snapshot.dirty === 'none') {
    return {
      dirty: 'none',
      rows: snapshot?.rows ?? 0,
      cols: snapshot?.cols ?? 0,
      html: '',
      lineHtml: undefined,
      dirtyLines: undefined,
      cursorCol: snapshot?.cursorCol ?? 0,
      cursorRow: snapshot?.cursorRow ?? 0,
      cursorVisible: snapshot?.cursorVisible ?? false,
    };
  }

  return {
    ...snapshot,
    // Prefer the native snapshot HTML when provided. The Android bridge now
    // resolves per-cell styles itself so tmux status lines and ANSI/RGB spans
    // render consistently in the WebView.
    html: typeof snapshot.html === 'string'
      ? snapshot.html
      : ZenTerminalVt.getVisibleHtml(handle),
  };
}

/**
 * Backward-compatible alias for older call sites and stale Metro caches.
 */
export const getRenderState = getRenderSnapshot;

/**
 * Get the full visible text as a string (for text selection / copy).
 */
export function getVisibleText(handle: number): string {
  return ZenTerminalVt.getVisibleText(handle);
}

/**
 * Get the full visible screen formatted as HTML.
 */
export function getVisibleHtml(handle: number): string {
  return ZenTerminalVt.getVisibleHtml(handle);
}

export function getCrashBreadcrumb(): NativeCrashBreadcrumb | null {
  const breadcrumb = ZenTerminalVt.getCrashBreadcrumb();
  if (!breadcrumb || typeof breadcrumb !== 'object' || !('stage' in breadcrumb)) {
    return null;
  }
  return breadcrumb as NativeCrashBreadcrumb;
}

export function clearCrashBreadcrumb(): void {
  ZenTerminalVt.clearCrashBreadcrumb();
}
