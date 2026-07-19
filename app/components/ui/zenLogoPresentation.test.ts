import { describe, expect, test } from "bun:test";
import { relativeLuminance } from "../../theme/colorUtils";
import {
  ZEN_BRAND_COLORS,
  ZEN_DARK_APP_COLORS,
  ZEN_LIGHT_APP_COLORS,
} from "../../theme/primitives";
import { resolveZenLogoDetailTint } from "./zenLogoPresentation";

function contrastRatio(foreground: string, background: string): number {
  const foregroundLuminance = relativeLuminance(foreground);
  const backgroundLuminance = relativeLuminance(background);
  const lighter = Math.max(foregroundLuminance, backgroundLuminance);
  const darker = Math.min(foregroundLuminance, backgroundLuminance);
  return (lighter + 0.05) / (darker + 0.05);
}

describe("Zen logo detail contrast", () => {
  test("keeps the accepted dark-theme ivory detail unmodified", () => {
    expect(ZEN_DARK_APP_COLORS.logoDetail).toBe(ZEN_BRAND_COLORS.ivory);
    expect(resolveZenLogoDetailTint(ZEN_DARK_APP_COLORS)).toBeUndefined();

    for (const background of [
      ZEN_DARK_APP_COLORS.bgPrimary,
      ZEN_DARK_APP_COLORS.bgSurface,
      ZEN_DARK_APP_COLORS.accentSoft,
    ]) {
      expect(
        contrastRatio(ZEN_DARK_APP_COLORS.logoDetail, background),
      ).toBeGreaterThanOrEqual(7);
    }
  });

  test("tints the detail to high-contrast dark sage on light surfaces", () => {
    expect(resolveZenLogoDetailTint(ZEN_LIGHT_APP_COLORS)).toBe(
      ZEN_LIGHT_APP_COLORS.logoDetail,
    );
    expect(ZEN_LIGHT_APP_COLORS.logoDetail).not.toBe(
      ZEN_BRAND_COLORS.ivory,
    );
    expect(
      contrastRatio(
        ZEN_LIGHT_APP_COLORS.logoDetail,
        ZEN_BRAND_COLORS.sage,
      ),
    ).toBeGreaterThanOrEqual(4.5);

    for (const background of [
      ZEN_LIGHT_APP_COLORS.bgPrimary,
      ZEN_LIGHT_APP_COLORS.bgSurface,
      ZEN_LIGHT_APP_COLORS.bgElevated,
      ZEN_LIGHT_APP_COLORS.accentSoft,
    ]) {
      expect(
        contrastRatio(ZEN_LIGHT_APP_COLORS.logoDetail, background),
      ).toBeGreaterThanOrEqual(7);
    }
  });
});
