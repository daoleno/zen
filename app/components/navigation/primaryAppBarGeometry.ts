export const PRIMARY_APP_BAR_HEIGHT = 52;
export const PRIMARY_APP_BAR_LAYOUT_MODE = "overlay" as const;

export interface PrimaryAppBarGeometry {
  readonly appBarHeight: typeof PRIMARY_APP_BAR_HEIGHT;
  readonly contentInset: number;
  readonly layoutMode: typeof PRIMARY_APP_BAR_LAYOUT_MODE;
  readonly safeAreaTop: number;
}

// Structural chrome geometry is intentionally independent of route and width.
export function resolvePrimaryAppBarGeometry(
  safeAreaTop: number,
): PrimaryAppBarGeometry {
  return {
    appBarHeight: PRIMARY_APP_BAR_HEIGHT,
    contentInset: safeAreaTop + PRIMARY_APP_BAR_HEIGHT,
    layoutMode: PRIMARY_APP_BAR_LAYOUT_MODE,
    safeAreaTop,
  };
}
