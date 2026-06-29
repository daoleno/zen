export const FLOATING_TAB_BAR_HEIGHT = 54;
export const FLOATING_TAB_BAR_HORIZONTAL_INSET = 16;
export const FLOATING_TAB_BAR_BOTTOM_GAP = 10;
const COMPOSER_TAB_BAR_SPACING = 8;

/** Space to reserve above the floating tab bar so composers stay visible. */
export function getFloatingTabBarInset(safeAreaBottom: number): number {
  const bottom = Math.max(safeAreaBottom, FLOATING_TAB_BAR_BOTTOM_GAP);
  return bottom + FLOATING_TAB_BAR_HEIGHT + COMPOSER_TAB_BAR_SPACING;
}