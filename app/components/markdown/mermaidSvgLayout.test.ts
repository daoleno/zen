import { describe, expect, test } from "bun:test";
import { mermaidPreviewLayout, parseMermaidSvgSize } from "./mermaidSvgLayout";

describe("Mermaid preview layout", () => {
  test("reads viewBox and fits to a 390-wide phone without exploding height", () => {
    const size = parseMermaidSvgSize(
      `<svg viewBox="0 0 800 1200"></svg>`,
      { width: 100, height: 100 },
    );
    expect(size).toEqual({ width: 800, height: 1200 });
    const layout = mermaidPreviewLayout(size, 390, 280);
    expect(layout.width).toBe(390);
    expect(layout.height).toBe(280);
    expect(layout.overflow).toBe(true);
  });
});
