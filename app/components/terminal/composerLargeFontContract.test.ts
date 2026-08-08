import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

describe("Composer large-font contract", () => {
  test("Composer text caps at the deterministic font scale", () => {
    const input = source("InterfaceComposerInput.tsx");
    const chip = source("ComposerModelChip.tsx");

    expect(input).toContain(
      "maxFontSizeMultiplier={COMPOSER_MAX_FONT_SCALE}",
    );
    expect(chip).toContain(
      "maxFontSizeMultiplier={COMPOSER_MAX_FONT_SCALE}",
    );
  });

  test("the chip renders one truncated line and keeps the full label accessible", () => {
    const chip = source("ComposerModelChip.tsx");

    expect(chip).toContain('numberOfLines={1}');
    expect(chip).toContain('ellipsizeMode="tail"');
    expect(chip).toContain("accessibilityLabel={accessibilityLabel}");
    expect(chip).toContain(
      "height: COMPOSER_MODEL_CHIP_HEIGHT,",
    );
  });

  test("text containers never clip vertically at the font cap", () => {
    const input = source("InterfaceComposerInput.tsx");
    const chip = source("ComposerModelChip.tsx");

    expect(input).not.toMatch(/inputWrap: \{[\s\S]*?overflow/);
    expect(input).not.toMatch(/placeholderOverlay: \{[\s\S]*?overflow/);
    expect(chip).not.toContain('overflow: "hidden"');
    expect(chip).toContain(
      "COMPOSER_MODEL_CHIP_LABEL_LINE_HEIGHT",
    );
  });

  test("the dock slot matches the chip height exactly", () => {
    const dock = source("InterfaceComposerExpandingDock.tsx");
    expect(dock).toContain("height: COMPOSER_MODEL_CHIP_HEIGHT,");
  });
});
