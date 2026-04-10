import { requireNativeModule } from 'expo-modules-core';

const ZenTerminalVt = requireNativeModule('ZenTerminalVt');

/**
 * Cell flags bitmask constants.
 */
export const CellFlags = {
  BOLD: 1 << 0,
  ITALIC: 1 << 1,
  UNDERLINE: 1 << 2,
  STRIKETHROUGH: 1 << 3,
  INVERSE: 1 << 4,
  WIDE: 1 << 5,
} as const;

/**
 * Render state returned from the native module.
 *
 * `cells` is a flat Int32Array packed as [codepoint, fg, bg, flags] per cell,
 * row-major order. Total length = rows * cols * 4.
 *
 * Colors are packed ARGB (0xAARRGGBB). bg=0 means default background.
 */
export interface RenderState {
  dirty: 'none' | 'partial' | 'full';
  rows: number;
  cols: number;
  cells: number[];
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
 * Extract the current render state (cell grid with colors and styles).
 * Returns dirty='none' if nothing changed since last call.
 */
export function getRenderState(handle: number): RenderState {
  return ZenTerminalVt.getRenderState(handle);
}

/**
 * Get the full visible text as a string (for text selection / copy).
 */
export function getVisibleText(handle: number): string {
  return ZenTerminalVt.getVisibleText(handle);
}

/**
 * Get the current visible screen as HTML with inline styles.
 * Intended for lightweight DOM rendering surfaces.
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
