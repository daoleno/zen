// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  ZEN_DARK_APP_COLORS,
  ZEN_LIGHT_APP_COLORS,
} from "../../theme/primitives";

function alphaFromCssColor(color: string): number | null {
  const match = color.match(
    /^rgba\(\s*[\d.]+\s*,\s*[\d.]+\s*,\s*[\d.]+\s*,\s*([\d.]+)\s*\)$/i,
  );
  if (match == null) {
    return null;
  }
  return Number(match[1]);
}

describe("primary shell Light-mode tokens", () => {
  test("drawer scrim stays translucent in Light and Dark", () => {
    const lightAlpha = alphaFromCssColor(ZEN_LIGHT_APP_COLORS.modalBackdrop);
    const darkAlpha = alphaFromCssColor(ZEN_DARK_APP_COLORS.modalBackdrop);
    expect(lightAlpha).not.toBeNull();
    expect(darkAlpha).not.toBeNull();
    expect(lightAlpha!).toBeGreaterThan(0);
    expect(lightAlpha!).toBeLessThan(1);
    expect(darkAlpha!).toBeGreaterThan(0);
    expect(darkAlpha!).toBeLessThan(1);
  });

  test("Light chrome surfaces stay light and distinct from text", () => {
    expect(ZEN_LIGHT_APP_COLORS.bgPrimary).toBe("#F7F8F6");
    expect(ZEN_LIGHT_APP_COLORS.bgSurface).toBe("#FFFFFF");
    expect(ZEN_LIGHT_APP_COLORS.surfacePressed).toBe("#E4E8E3");
    expect(ZEN_LIGHT_APP_COLORS.textPrimary).toBe("#171A18");
    expect(ZEN_LIGHT_APP_COLORS.textPrimary).not.toBe(
      ZEN_LIGHT_APP_COLORS.bgPrimary,
    );
    expect(ZEN_LIGHT_APP_COLORS.accent).toBe("#56705C");
  });

  test("pressed and subtle surfaces differ from the primary canvas", () => {
    expect(ZEN_LIGHT_APP_COLORS.surfaceSubtle).not.toBe(
      ZEN_LIGHT_APP_COLORS.bgPrimary,
    );
    expect(ZEN_LIGHT_APP_COLORS.surfacePressed).not.toBe(
      ZEN_LIGHT_APP_COLORS.bgPrimary,
    );
    expect(ZEN_LIGHT_APP_COLORS.borderSubtle).not.toBe(
      ZEN_LIGHT_APP_COLORS.bgPrimary,
    );
  });
});
