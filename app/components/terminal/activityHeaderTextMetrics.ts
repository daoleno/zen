import type { TextStyle } from "react-native";
import { TypeScale, Typography, UiTextMetrics } from "../../constants/tokens";

/**
 * Collapsed Tool activity header text metrics.
 *
 * Source Han Sans title and Maple Mono command/detail must share one stable
 * line box and visual center with the tone icon and disclosure chevron —
 * whether the row has no detail, a short command, or a long ellipsized path.
 *
 * Ownership is shared fontSize/lineHeight plus UiTextMetrics (declared line
 * box, no Android font padding). Do not use platform-only margins, transforms,
 * per-string nudges, or arbitrary row inflation.
 */
export const ACTIVITY_HEADER_FONT_SIZE = TypeScale.caption.fontSize;
export const ACTIVITY_HEADER_LINE_HEIGHT = TypeScale.caption.lineHeight;
export const ACTIVITY_HEADER_LETTER_SPACING = TypeScale.caption.letterSpacing;
/** Gap between the human title and monospace detail. */
export const ACTIVITY_HEADER_DETAIL_GAP = 6;
/** Tone-icon slot edge; stays within 1px of the text line box for shared center. */
export const ACTIVITY_HEADER_ICON_SLOT = 18;
export const ACTIVITY_HEADER_ROW_MIN_HEIGHT = 28;
export const ACTIVITY_HEADER_ROW_PADDING_VERTICAL = 2;

export const ACTIVITY_HEADER_TITLE_FONT = Typography.uiFontMedium;
export const ACTIVITY_HEADER_DETAIL_FONT = Typography.terminalFont;

/** Shared Text style applied to both title and detail. */
export const activityHeaderSharedTextStyle: Pick<
  TextStyle,
  | "fontSize"
  | "lineHeight"
  | "letterSpacing"
  | "includeFontPadding"
  | "textAlignVertical"
> = {
  fontSize: ACTIVITY_HEADER_FONT_SIZE,
  lineHeight: ACTIVITY_HEADER_LINE_HEIGHT,
  letterSpacing: ACTIVITY_HEADER_LETTER_SPACING,
  ...UiTextMetrics,
};

/** One vertical line box for the copy row (title only or title + detail). */
export function activityHeaderCopyLineBoxHeight(
  lineHeight: number = ACTIVITY_HEADER_LINE_HEIGHT,
): number {
  return lineHeight;
}

/**
 * True when the text line box and icon slot share a visual center under
 * `alignItems: "center"` without inflating the row past the declared min height.
 */
export function activityHeaderCentersWithIconSlot(
  lineBoxHeight: number = ACTIVITY_HEADER_LINE_HEIGHT,
  iconSlot: number = ACTIVITY_HEADER_ICON_SLOT,
  rowMinHeight: number = ACTIVITY_HEADER_ROW_MIN_HEIGHT,
  rowPaddingVertical: number = ACTIVITY_HEADER_ROW_PADDING_VERTICAL,
): boolean {
  const contentBudget = rowMinHeight - rowPaddingVertical * 2;
  return (
    Math.abs(lineBoxHeight - iconSlot) <= 1 &&
    lineBoxHeight <= contentBudget &&
    iconSlot <= contentBudget
  );
}
