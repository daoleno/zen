import { describe, expect, test } from "bun:test";
import { relativeLuminance } from "../../theme/colorUtils";
import {
  ZEN_DARK_APP_COLORS,
  ZEN_DARK_MATERIALS,
  ZEN_LIGHT_APP_COLORS,
  ZEN_LIGHT_MATERIALS,
} from "../../theme/primitives";

function alphaFromCssColor(color: string): number | null {
  const match = color.match(
    /^rgba\(\s*[\d.]+\s*,\s*[\d.]+\s*,\s*[\d.]+\s*,\s*([\d.]+)\s*\)$/i,
  );
  return match == null ? null : Number(match[1]);
}

function contrast(foreground: string, background: string): number {
  const a = relativeLuminance(foreground);
  const b = relativeLuminance(background);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

describe("shell tokens stay legible in Light and Dark", () => {
  test("scrims and chrome materials stay translucent", () => {
    for (const color of [
      ZEN_LIGHT_APP_COLORS.modalBackdrop,
      ZEN_DARK_APP_COLORS.modalBackdrop,
      ZEN_LIGHT_MATERIALS.chrome,
      ZEN_DARK_MATERIALS.chrome,
    ]) {
      const alpha = alphaFromCssColor(color);
      expect(alpha).not.toBeNull();
      expect(alpha!).toBeGreaterThan(0);
      expect(alpha!).toBeLessThan(1);
    }
  });

  test("chrome over scrolling text is dense enough to read through", () => {
    for (const materials of [ZEN_LIGHT_MATERIALS, ZEN_DARK_MATERIALS]) {
      for (const fill of [materials.chrome, materials.regular, materials.thick]) {
        expect(alphaFromCssColor(fill)!).toBeGreaterThanOrEqual(0.85);
      }
    }
  });

  test("text roles meet WCAG AA on every canvas", () => {
    for (const colors of [ZEN_LIGHT_APP_COLORS, ZEN_DARK_APP_COLORS]) {
      for (const background of [colors.bgPrimary, colors.bgSurface, colors.bgElevated]) {
        expect(contrast(colors.textPrimary, background)).toBeGreaterThanOrEqual(7);
        expect(contrast(colors.textSecondary, background)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(colors.textTertiary, background)).toBeGreaterThanOrEqual(4.5);
      }
      expect(contrast(colors.textOnAccent, colors.accent)).toBeGreaterThanOrEqual(4.5);
      expect(contrast(colors.dangerText, colors.bgPrimary)).toBeGreaterThanOrEqual(4.5);
    }
  });

  test("pressed and subtle surfaces differ from the canvas", () => {
    for (const colors of [ZEN_LIGHT_APP_COLORS, ZEN_DARK_APP_COLORS]) {
      expect(colors.surfaceSubtle).not.toBe(colors.bgPrimary);
      expect(colors.surfacePressed).not.toBe(colors.bgPrimary);
      expect(colors.borderSubtle).not.toBe(colors.bgPrimary);
    }
  });
});
