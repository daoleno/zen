import type { AppColors } from "../../theme/palette";
import { ZEN_BRAND_COLORS } from "../../theme/primitives";

type ZenLogoColors = Pick<AppColors, "logoDetail">;

/**
 * The dark artwork already contains the accepted ivory detail. A tint is only
 * needed when the resolved theme requests a different contrast semantic.
 */
export function resolveZenLogoDetailTint(
  colors: ZenLogoColors,
): string | undefined {
  return colors.logoDetail === ZEN_BRAND_COLORS.ivory
    ? undefined
    : colors.logoDetail;
}
