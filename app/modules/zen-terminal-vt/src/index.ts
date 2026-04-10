import { requireNativeModule } from 'expo-modules-core';

type NativeRenderSnapshot = Omit<RenderSnapshot, 'html'> & {
  html?: string;
};

interface NativeTerminalVtModule {
  createTerminal(cols: number, rows: number): number;
  destroyTerminal(handle: number): void;
  writeData(handle: number, data: string): void;
  resize(handle: number, cols: number, rows: number, cellWidth: number, cellHeight: number): void;
  getRenderSnapshot?: (handle: number) => NativeRenderSnapshot;
  getRenderState?: (handle: number) => NativeRenderSnapshot;
  getVisibleText(handle: number): string;
  getVisibleHtml(handle: number): string;
  getCrashBreadcrumb(): NativeCrashBreadcrumb | null;
  clearCrashBreadcrumb(): void;
}

const ZenTerminalVt = requireNativeModule<NativeTerminalVtModule>('ZenTerminalVt');

export interface RenderSnapshot {
  dirty: 'none' | 'partial' | 'full';
  rows: number;
  cols: number;
  html: string;
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
      cursorCol: snapshot?.cursorCol ?? 0,
      cursorRow: snapshot?.cursorRow ?? 0,
      cursorVisible: snapshot?.cursorVisible ?? false,
    };
  }

  return {
    ...snapshot,
    html: ZenTerminalVt.getVisibleHtml(handle),
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
