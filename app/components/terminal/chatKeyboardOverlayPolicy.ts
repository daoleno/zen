export interface StructuredChatOverlayGeometryInput {
  canvasHeight: number;
  composerHeight: number;
  keyboardHeight: number;
  keyboardVerticalOffset?: number;
}

export interface StructuredChatOverlayGeometry {
  composerTop: number;
  scrollClearance: number;
  timelineHeight: number;
  contentOffsetDelta: 0;
}

export type StructuredChatInsetPlatform = "ios" | "android" | "web";

export interface StructuredChatEffectiveClearanceInput {
  platform: StructuredChatInsetPlatform;
  requestedClearance: number;
  rawOffset: number;
  previousClearance: number;
}

export interface StructuredChatContentFadeGeometry {
  opaqueBottomInset: number;
  transparentBottomInset: number;
  fadeHeight: number;
}

const CONTENT_FADE_START = 0.5;
const CONTENT_FADE_END = 0.9;

export function structuredChatOverlayTranslateY(
  keyboardTranslation: number,
  keyboardProgress: number,
  keyboardVerticalOffset: number,
) {
  "worklet";
  return (
    keyboardTranslation +
    Math.max(0, Math.min(1, keyboardProgress)) * keyboardVerticalOffset
  );
}

export function structuredChatScrollClearance(
  composerHeight: number,
  overlayTranslateY: number,
) {
  "worklet";
  return Math.max(0, composerHeight) + Math.max(0, -overlayTranslateY);
}

/**
 * Describes an alpha-only fade in bottom-inset coordinates. Timeline pixels
 * stay opaque through the first half of the measured Composer, fade through
 * 90% of its footprint, and remain transparent below it. Keyboard lift uses
 * the exact same translation as the floating Composer and scroll clearance.
 */
export function structuredChatContentFadeGeometry(
  composerHeight: number,
  overlayTranslateY: number,
): StructuredChatContentFadeGeometry {
  "worklet";
  const height = Math.max(0, composerHeight);
  const overlayLift = Math.max(0, -overlayTranslateY);
  const opaqueBottomInset =
    overlayLift + height * (1 - CONTENT_FADE_START);
  const transparentBottomInset =
    overlayLift + height * (1 - CONTENT_FADE_END);

  return {
    opaqueBottomInset,
    transparentBottomInset,
    fadeHeight: opaqueBottomInset - transparentBottomInset,
  };
}

export function structuredChatLogicalOffset(
  rawOffset: number,
  clearance: number,
  platform: StructuredChatInsetPlatform,
) {
  "worklet";
  return platform === "android"
    ? rawOffset - clearance
    : rawOffset;
}

/**
 * A shrinking overlay may not remove range currently occupied by the reader.
 * Keeping that range makes native clamping—and therefore anchor jumps—an
 * unrepresentable state. The inset contracts once the reader scrolls back
 * inside the requested range.
 */
export function structuredChatEffectiveClearance({
  platform,
  requestedClearance,
  rawOffset,
  previousClearance,
}: StructuredChatEffectiveClearanceInput) {
  "worklet";
  const requested = Math.max(0, requestedClearance);
  if (platform === "web") {
    return requested;
  }
  const logicalOffset = structuredChatLogicalOffset(
    rawOffset,
    Math.max(0, previousClearance),
    platform,
  );
  return Math.max(requested, Math.max(0, -logicalOffset));
}

export function structuredChatLatestOffset(
  clearance: number,
  platform: StructuredChatInsetPlatform,
) {
  "worklet";
  return platform === "ios" ? -Math.max(0, clearance) : 0;
}

/**
 * A keyboard or Composer change extends the range after timeline content. It
 * never changes the canvas height or current content offset.
 */
export function structuredChatOverlayGeometry({
  canvasHeight,
  composerHeight,
  keyboardHeight,
  keyboardVerticalOffset = 0,
}: StructuredChatOverlayGeometryInput): StructuredChatOverlayGeometry {
  const effectiveKeyboardHeight = Math.max(
    0,
    keyboardHeight - keyboardVerticalOffset,
  );
  const safeComposerHeight = Math.max(0, composerHeight);
  const scrollClearance = structuredChatScrollClearance(
    safeComposerHeight,
    -effectiveKeyboardHeight,
  );

  return {
    composerTop:
      canvasHeight - effectiveKeyboardHeight - safeComposerHeight,
    scrollClearance,
    timelineHeight: canvasHeight,
    contentOffsetDelta: 0,
  };
}
