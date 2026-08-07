import { describe, expect, test } from "bun:test";
import { TERMINAL_GRID_FONT_SIZE_CSS_PX } from "./terminalFontDensity";
import { buildGhosttyTerminalHtml } from "./ghosttyWebViewHtml";

const theme = {
  background: "#000000",
  foreground: "#ffffff",
  cursor: "#ffffff",
  selectionBackground: "#333333",
} as Parameters<typeof buildGhosttyTerminalHtml>[0];

describe("Ghostty WebView bootstrap boundary", () => {
  test("an invalid font size falls back to the compact grid default token", () => {
    const html = buildGhosttyTerminalHtml(theme, null, NaN, 0);

    expect(html).toContain(`font-size: ${TERMINAL_GRID_FONT_SIZE_CSS_PX}px;`);
    expect(html).toContain(
      `const FONT_SIZE = ${TERMINAL_GRID_FONT_SIZE_CSS_PX};`,
    );
  });
  test("a missing bundled font produces usable fallback HTML", () => {
    const html = buildGhosttyTerminalHtml(theme, null, 13, 0);

    expect(html).not.toContain("@font-face");
    expect(html).toContain("font-family: monospace");
    expect(html).toContain("send({ type: 'ready' });");
  });

  test("the terminal pins its font density so OS text scaling cannot inflate the grid", () => {
    const html = buildGhosttyTerminalHtml(
      theme,
      "https://zen.local/font.ttf",
      13,
      0,
    );

    expect(html).toContain("-webkit-text-size-adjust: none;");
    expect(html).toContain("text-size-adjust: none;");
    expect(html).toContain("const gridSize = (viewportWidth, viewportHeight, cellWidth, cellHeight)");
  });

  test("inline bootstrap failures and font timeout are observable and bounded", () => {
    const html = buildGhosttyTerminalHtml(
      theme,
      "https://zen.local/font.ttf",
      13,
      0,
    );

    expect(html).toContain("'bootstrapError'");
    expect(html).toContain("type: 'bootstrapWarning'");
    expect(html).toContain("unhandledrejection");
    expect(html).toContain("FONT_READY_TIMEOUT_MS");
    expect(html).toContain("Promise.race");
  });

  test("the exact generated inline source is valid standalone JavaScript", () => {
    const html = buildGhosttyTerminalHtml(theme, null, 13, 7);
    const script = html.match(/<script>([\s\S]*)<\/script>/)?.[1];

    expect(script).toBeDefined();
    expect(() => new Function(script!)).not.toThrow();
    expect(script!.match(/const cursor = document\.getElementById/g)).toHaveLength(1);
    expect(script).toContain("const RENDERER_GENERATION = 7;");
    expect(script).toContain("payload.rendererGeneration = RENDERER_GENERATION;");
    expect(script!.indexOf("type: 'resize'")).toBeLessThan(
      script!.indexOf("type: 'ready'"),
    );
  });
});
