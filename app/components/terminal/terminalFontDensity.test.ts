import { describe, expect, test } from "bun:test";
import {
  TERMINAL_GRID_SIZE_SOURCE,
  TERMINAL_TEXT_SIZE_ADJUST_CSS,
  TERMINAL_WEBVIEW_TEXT_ZOOM_PERCENT,
  terminalGridSize,
  terminalWebViewDensityProps,
} from "./terminalFontDensity";

describe("Terminal font-density contract", () => {
  test("Android pins WebView text zoom to 100% so the system font scale cannot inflate the terminal grid", () => {
    expect(TERMINAL_WEBVIEW_TEXT_ZOOM_PERCENT).toBe(100);
    expect(terminalWebViewDensityProps("android")).toEqual({ textZoom: 100 });
  });

  test("iOS relies on WKWebView CSS pixels and needs no adjustment to share the contract", () => {
    expect(terminalWebViewDensityProps("ios")).toEqual({});
    expect(terminalWebViewDensityProps("web")).toEqual({});
  });

  test("grid size follows the actual rendered cell metrics, not a fixed row budget", () => {
    expect(terminalGridSize(390, 700, 8, 17)).toEqual({ cols: 48, rows: 41 });
    expect(terminalGridSize(390, 700, 10.4, 17)).toEqual({ cols: 37, rows: 41 });
    expect(terminalGridSize(844, 390, 8, 17)).toEqual({ cols: 105, rows: 22 });
  });

  test("an empty viewport or glyph metrics cannot produce an infinite or zero grid", () => {
    expect(terminalGridSize(0, 0, 8, 17)).toEqual({ cols: 1, rows: 1 });
    const guarded = terminalGridSize(390, 700, 0, 0);
    expect(guarded.cols).toBeGreaterThanOrEqual(1);
    expect(guarded.rows).toBeGreaterThanOrEqual(1);
    expect(Number.isFinite(guarded.cols)).toBe(true);
    expect(Number.isFinite(guarded.rows)).toBe(true);
  });

  test("the embedded renderer source stays equivalent to the TypeScript grid twin", () => {
    const embedded = new Function(
      TERMINAL_GRID_SIZE_SOURCE + "; return gridSize;",
    )() as typeof terminalGridSize;

    const samples: Array<[number, number, number, number]> = [
      [390, 700, 8, 17],
      [390, 700, 10.4, 17],
      [844, 390, 8, 17],
      [320, 480, 7.5, 15],
      [0, 0, 8, 17],
      [390, 700, 0, 0],
    ];
    for (const sample of samples) {
      expect(embedded(...sample)).toEqual(terminalGridSize(...sample));
    }
  });

  test("the no-inflation CSS contract is explicit in the shared stylesheet", () => {
    expect(TERMINAL_TEXT_SIZE_ADJUST_CSS).toContain(
      "-webkit-text-size-adjust: 100%;",
    );
    expect(TERMINAL_TEXT_SIZE_ADJUST_CSS).toContain(
      "-webkit-text-size-adjust: none;",
    );
    expect(TERMINAL_TEXT_SIZE_ADJUST_CSS).toContain("text-size-adjust: 100%;");
    expect(TERMINAL_TEXT_SIZE_ADJUST_CSS).toContain("text-size-adjust: none;");
  });
});
