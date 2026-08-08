import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  MOBILE_SINGLE_LINE_INPUT_LAYOUT,
  mobileSingleLinePlaceholderFitsControl,
  mobileSingleLineScaledLineHeight,
  mobileSingleLineTextInsets,
  mobileSingleLineTextLaneWidth,
} from "./mobileSingleLineInputModel";

describe("single-line input geometry invariant", () => {
  test("placeholder, text, secure text, and scaling share one stable control height", () => {
    const layout = MOBILE_SINGLE_LINE_INPUT_LAYOUT;
    expect(layout.controlHeight).toBe(48);
    expect(layout.fontSize).toBe(15);
    expect(layout.lineHeight).toBe(23);
    expect(layout.maximumFontSizeMultiplier).toBe(1.5);
    expect(layout.horizontalPadding).toBe(14);
    expect(layout.accessoryLaneWidth).toBe(44);
    expect(layout.minimumTextLaneWidth).toBe(96);
  });

  test("scaled line height never exceeds the control height", () => {
    for (const fontScale of [1, 1.2, 1.5, 2, 3]) {
      const scaled = mobileSingleLineScaledLineHeight(fontScale);
      expect(scaled).toBeLessThanOrEqual(MOBILE_SINGLE_LINE_INPUT_LAYOUT.controlHeight);
      expect(mobileSingleLinePlaceholderFitsControl(fontScale)).toBe(true);
    }
  });

  test("Android large-font (1.3) and iOS accessibility metrics stay vertically centered", () => {
    const androidLargeFont = mobileSingleLineScaledLineHeight(1.3);
    expect(androidLargeFont).toBeCloseTo(29.9, 1);
    expect(androidLargeFont).toBeLessThanOrEqual(
      MOBILE_SINGLE_LINE_INPUT_LAYOUT.controlHeight,
    );
    expect(mobileSingleLineScaledLineHeight(1.15)).toBe(23 * 1.15);
    expect(mobileSingleLineScaledLineHeight(1)).toBe(23);
    expect(mobileSingleLinePlaceholderFitsControl(1.3)).toBe(true);
    expect(mobileSingleLinePlaceholderFitsControl(1.5)).toBe(true);
  });

  test("placeholder text lane fits minimum widths at the bounded maximum scale", () => {
    const lane = mobileSingleLineTextLaneWidth(320, true, true);
    expect(lane).toBe(320 - 44 - 14 - 44);
    expect(lane).toBeGreaterThanOrEqual(
      MOBILE_SINGLE_LINE_INPUT_LAYOUT.minimumTextLaneWidth,
    );
  });

  test("bounded scaling caps the multiplier at the layout maximum", () => {
    expect(mobileSingleLineScaledLineHeight(3)).toBe(
      MOBILE_SINGLE_LINE_INPUT_LAYOUT.lineHeight *
        MOBILE_SINGLE_LINE_INPUT_LAYOUT.maximumFontSizeMultiplier,
    );
    expect(mobileSingleLineScaledLineHeight(0)).toBe(0);
  });

  test("text lane and placeholder overlay share identical insets", () => {
    for (const [leading, trailing] of [
      [false, false],
      [true, false],
      [false, true],
      [true, true],
    ]) {
      const insets = mobileSingleLineTextInsets(leading, trailing);
      expect(insets.left).toBe(leading ? 44 + 14 : 14);
      expect(insets.right).toBe(trailing ? 44 : 14);
      const lane = mobileSingleLineTextLaneWidth(320, leading, trailing);
      expect(lane).toBe(320 - insets.left - insets.right);
    }
  });
});

describe("shared input source contract", () => {
  const source = readFileSync(
    join(import.meta.dir, "./MobileSingleLineInput.tsx"),
    "utf8",
  );

  test("is the single TextInput owner without per-screen padding hacks", () => {
    const textInputCount = (source.match(/<TextInput\b/g) ?? []).length;
    expect(textInputCount).toBe(1);
    expect(source).not.toMatch(/paddingVertical:\s*[1-9]/);
    expect(source).toContain("textAlignVertical: \"center\"");
    expect(source).toContain("height: MOBILE_SINGLE_LINE_INPUT_LAYOUT.controlHeight");
    expect(source).toContain("paddingVertical: 0");
  });

  test("controlled inputs detach the native placeholder and render a centered overlay", () => {
    expect(source).toContain('placeholder={controlled ? undefined : placeholder}');
    expect(source).toContain("showPlaceholderOverlay");
    expect(source).toContain("pointerEvents=\"none\"");
    expect(source).toMatch(
      /placeholderSlot:\s*\{[^}]*top: 0,[^}]*bottom: 0,[^}]*justifyContent: "center"/s,
    );
    expect(source).toContain('"value" in inputProps');
    expect(source).toContain("numberOfLines={1}");
  });
});
