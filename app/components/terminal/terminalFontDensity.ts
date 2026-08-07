/**
 * One intentional font-density contract for the terminal on every platform.
 *
 * The terminal is a fixed glyph grid: its CSS font size is the single source
 * of truth and its PTY rows/columns are derived from the rendered glyph
 * metrics. The OS accessibility text scale must never silently rescale this
 * grid, or Android and iOS render at different densities and provider TUIs
 * wrap at different column counts for the same device width.
 *
 * Android WebView multiplies its default text zoom by the system font scale
 * when the WebView is created (AwSettings: "By default, scale the text size
 * by the system font scale factor. Embedders may override this by invoking
 * setTextZoom()"). Pinning text zoom to 100% keeps the terminal at the
 * intended CSS pixel density. iOS WKWebView does not apply Dynamic Type to
 * web content, so iOS needs no adjustment and shares the same contract.
 */

export const TERMINAL_WEBVIEW_TEXT_ZOOM_PERCENT = 100;

export interface TerminalWebViewDensityProps {
  textZoom?: number;
}

/**
 * Platform props that keep the terminal WebView at the intentional density.
 * Testable pure function; the terminal surface passes the current Platform.OS.
 */
export function terminalWebViewDensityProps(
  platform: string,
): TerminalWebViewDensityProps {
  return platform === 'android'
    ? { textZoom: TERMINAL_WEBVIEW_TEXT_ZOOM_PERCENT }
    : {};
}

/**
 * CSS that pins the terminal to its CSS pixel density on both renderers.
 * Prevents WebKit/Chromium text-size-adjust inflation heuristics from ever
 * rescaling the grid, and makes the no-inflation policy explicit in the DOM.
 * Both spellings are declared so every WebKit/Blink variant accepts at least
 * one and the last valid declaration wins.
 */
export const TERMINAL_TEXT_SIZE_ADJUST_CSS = `
  -webkit-text-size-adjust: 100%;
  -webkit-text-size-adjust: none;
  text-size-adjust: 100%;
  text-size-adjust: none;
`;

export interface TerminalGridSize {
  cols: number;
  rows: number;
}

/**
 * Canonical, self-contained JavaScript embedded directly into the WebView
 * document. The inline script uses this exact source so tests execute the
 * same geometry computation the renderer runs.
 */
export const TERMINAL_GRID_SIZE_SOURCE = `const gridSize = (viewportWidth, viewportHeight, cellWidth, cellHeight) => {
  const cols = Math.max(1, Math.floor(viewportWidth / Math.max(1, cellWidth)));
  const rows = Math.max(1, Math.floor(viewportHeight / Math.max(1, cellHeight)));
  return { cols, rows };
};`;

/**
 * Plain TypeScript twin of the embedded gridSize computation. Tests verify
 * both stay equivalent so the app and the renderer can never drift.
 */
export function terminalGridSize(
  viewportWidth: number,
  viewportHeight: number,
  cellWidth: number,
  cellHeight: number,
): TerminalGridSize {
  return {
    cols: Math.max(1, Math.floor(viewportWidth / Math.max(1, cellWidth))),
    rows: Math.max(1, Math.floor(viewportHeight / Math.max(1, cellHeight))),
  };
}
