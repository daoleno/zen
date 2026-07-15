// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

describe("Interface table native gesture arbitration", () => {
  test("uses a direction-gated native gesture region instead of React state", () => {
    const source = readFileSync(
      join(import.meta.dir, "CodexMessageBlock.tsx"),
      "utf8",
    );

    expect(source).toContain("<GestureDetector gesture={tableHorizontalGesture}>");
    expect(source).toContain("Gesture.Simultaneous(");
    expect(source).toContain("Gesture.Pan()");
    expect(source).toContain(".activeOffsetX([-6, 6])");
    expect(source).toContain(".failOffsetY([-8, 8])");
    expect(source).toContain("Gesture.Native()");
    expect(source).not.toContain("onTouchStart");
    expect(source).not.toContain("swipeEnabled");
  });
});
