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
