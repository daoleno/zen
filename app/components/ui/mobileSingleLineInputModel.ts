export const MOBILE_SINGLE_LINE_INPUT_LAYOUT = {
  controlHeight: 48,
  fontSize: 15,
  lineHeight: 23,
  horizontalPadding: 14,
  accessoryLaneWidth: 44,
  maximumFontSizeMultiplier: 1.5,
  minimumTextLaneWidth: 96,
} as const;

export function mobileSingleLineTextLaneWidth(
  containerWidth: number,
  leading: boolean,
  trailing: boolean,
): number {
  const layout = MOBILE_SINGLE_LINE_INPUT_LAYOUT;
  const leftInset = leading
    ? layout.accessoryLaneWidth + layout.horizontalPadding
    : layout.horizontalPadding;
  const rightInset = trailing
    ? layout.accessoryLaneWidth
    : layout.horizontalPadding;
  return Math.max(0, containerWidth - leftInset - rightInset);
}

export function mobileSingleLineScaledLineHeight(fontScale: number): number {
  const boundedScale = Math.min(
    Math.max(fontScale, 0),
    MOBILE_SINGLE_LINE_INPUT_LAYOUT.maximumFontSizeMultiplier,
  );
  return MOBILE_SINGLE_LINE_INPUT_LAYOUT.lineHeight * boundedScale;
}

/**
 * Horizontal insets of the text lane, shared by entered text and the centered
 * placeholder overlay so both stay inside the same stable control width.
 */
export function mobileSingleLineTextInsets(
  leading: boolean,
  trailing: boolean,
): { left: number; right: number } {
  const layout = MOBILE_SINGLE_LINE_INPUT_LAYOUT;
  return {
    left: leading
      ? layout.accessoryLaneWidth + layout.horizontalPadding
      : layout.horizontalPadding,
    right: trailing ? layout.accessoryLaneWidth : layout.horizontalPadding,
  };
}

/**
 * Placeholder overlay must never exceed the control height even at the
 * bounded font scale, or its top edge clips again.
 */
export function mobileSingleLinePlaceholderFitsControl(
  fontScale: number,
): boolean {
  return (
    mobileSingleLineScaledLineHeight(fontScale) <=
    MOBILE_SINGLE_LINE_INPUT_LAYOUT.controlHeight
  );
}
