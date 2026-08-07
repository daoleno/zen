import { describe, expect, test } from "bun:test";
import {
  TERMINAL_GRID_CELL_WIDTH_FALLBACK_EM,
  TERMINAL_GRID_FONT_SIZE_CSS_PX,
  TERMINAL_GRID_LINE_HEIGHT_RATIO,
  TERMINAL_GRID_SIZE_SOURCE,
  TERMINAL_TEXT_SIZE_ADJUST_CSS,
  TERMINAL_WEBVIEW_TEXT_ZOOM_PERCENT,
  terminalGridCellWidthFallbackCssPx,
  terminalGridLineHeightCssPx,
  terminalGridSize,
  terminalWebViewDensityProps,
} from "./terminalFontDensity";

const MAPLE_MONO_LATIN_CELL_EM = 0.6;

describe("Terminal font-density contract", () => {
  test("the default grid font token is compact: 8 CSS px, separate from UI mono typography", () => {
    expect(TERMINAL_GRID_FONT_SIZE_CSS_PX).toBe(8);
    expect(TERMINAL_GRID_LINE_HEIGHT_RATIO).toBe(1.28);
    expect(terminalGridLineHeightCssPx(TERMINAL_GRID_FONT_SIZE_CSS_PX)).toBe(11);
    expect(TERMINAL_GRID_CELL_WIDTH_FALLBACK_EM).toBeCloseTo(0.62, 5);
  });

  test("a realistic 360dp portrait viewport gets a Termius-comparable column budget", () => {
    const cellWidth = TERMINAL_GRID_FONT_SIZE_CSS_PX * MAPLE_MONO_LATIN_CELL_EM;
    const grid = terminalGridSize(
      360,
      640,
      cellWidth,
      terminalGridLineHeightCssPx(TERMINAL_GRID_FONT_SIZE_CSS_PX),
    );

    expect(cellWidth).toBeCloseTo(4.8, 5);
    expect(grid.cols).toBeGreaterThanOrEqual(70);
    expect(grid.cols).toBeLessThanOrEqual(80);
    expect(grid.cols).toBe(75);
    expect(grid.rows).toBeGreaterThanOrEqual(50);
  });

  test("advertised columns fit the rendered cell metrics: the PTY is never lied to", () => {
    const cellWidth = TERMINAL_GRID_FONT_SIZE_CSS_PX * MAPLE_MONO_LATIN_CELL_EM;
    const viewportWidth = 360;
    const grid = terminalGridSize(viewportWidth, 640, cellWidth, 11);

    expect(grid.cols * cellWidth).toBeLessThanOrEqual(viewportWidth);
    expect((grid.cols + 1) * cellWidth).toBeGreaterThan(viewportWidth);
  });

  test("the fallback cell width stays inside the font's measured 0.6em cell", () => {
    const fallback = terminalGridCellWidthFallbackCssPx(TERMINAL_GRID_FONT_SIZE_CSS_PX);
    const measured = TERMINAL_GRID_FONT_SIZE_CSS_PX * MAPLE_MONO_LATIN_CELL_EM;
    expect(fallback).toBeCloseTo(4.96, 5);
    expect(Math.abs(fallback - measured) / measured).toBeLessThan(0.05);
  });

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
