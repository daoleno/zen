export const COMPOSER_ACTION_SLOT_WIDTH = 68;
export const COMPOSER_ACTION_HORIZONTAL_PADDING = 6;
export const COMPOSER_LEADING_ACTION_WIDTH = 44;
export const COMPOSER_CHATGPT_DETACHED_ACTION_GAP = 8;

export const COMPOSER_OUTER_HORIZONTAL_INSET = {
  chatgpt: 16,
  telegram: 10,
  classic: 12,
  ambient: 10,
} as const;

export const COMPOSER_PANEL_METRICS = {
  chatgpt: { left: 6, right: 6, gap: 2 },
  telegram: { left: 6, right: 6, gap: 2 },
  classic: { left: 5, right: 6, gap: 4 },
  ambient: { left: 4, right: 4, gap: 4 },
} as const;

export type ComposerActionLayout =
  | "chatgpt"
  | "telegram"
  | "classic"
  | "ambient";

// The action slot is the only trailing action width removed from the flexible
// input. The 44px leading action is always present in the current surface.
export function composerInputWidth(
  screenWidth: number,
  layout: ComposerActionLayout,
) {
  const outerInset = COMPOSER_OUTER_HORIZONTAL_INSET[layout];
  const panel = COMPOSER_PANEL_METRICS[layout];
  const gaps = layout === "chatgpt"
    ? panel.gap + COMPOSER_CHATGPT_DETACHED_ACTION_GAP
    : panel.gap * 2;
  return Math.max(
    0,
    screenWidth -
      outerInset * 2 -
      panel.left -
      panel.right -
      gaps -
      COMPOSER_LEADING_ACTION_WIDTH -
      COMPOSER_ACTION_SLOT_WIDTH,
  );
}
