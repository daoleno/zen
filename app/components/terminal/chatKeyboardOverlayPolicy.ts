import { withAlpha } from "./colorWithAlpha";

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

export interface StructuredChatKeyboardLifecycleGate {
  enabled: boolean;
  appActive: boolean;
  epoch: number;
  acceptedNativeSampleEpoch: number;
}

export type StructuredChatKeyboardLifecycleEvent =
  | { type: "set_enabled"; enabled: boolean }
  | { type: "app_state"; active: boolean }
  | { type: "native_sample"; height: number; progress: number }
  /**
   * Composer focus under current route/app ownership may bind KeyboardController's
   * current geometry to this epoch. That is ownership proof for already-open IME
   * state, not a stale-geometry fallback from a prior route.
   */
  | { type: "composer_focus_bind"; height: number; progress: number };

export interface StructuredChatGatedOverlayTranslateYInput {
  gate: StructuredChatKeyboardLifecycleGate;
  keyboardTranslation: number;
  keyboardProgress: number;
  keyboardVerticalOffset: number;
}

export interface StructuredChatContentFadeGeometry {
  opaqueBottomInset: number;
  transparentBottomInset: number;
  fadeHeight: number;
}

const CONTENT_FADE_START = 0.5;
const CONTENT_FADE_END = 0.9;

/**
 * Theme colors currently represent transparency as the exact "transparent" string.
 * Supporting future color representations remains a separate theme-contract P2.
 */
export function resolveInterfaceChatCanvasColor(
  appBackground: string,
  surface: string,
) {
  return appBackground === "transparent" ? surface : appBackground;
}

export function structuredChatNativeMaskColors(canvasColor: string) {
  return {
    visible: withAlpha(canvasColor, 1),
    hidden: withAlpha(canvasColor, 0),
  };
}

export function createStructuredChatKeyboardLifecycleGate({
  enabled,
  appActive,
}: {
  enabled: boolean;
  appActive: boolean;
}): StructuredChatKeyboardLifecycleGate {
  return {
    enabled,
    appActive,
    epoch: 1,
    acceptedNativeSampleEpoch: 0,
  };
}

export function structuredChatKeyboardGeometryIsOpen(
  height: number,
  progress: number,
) {
  "worklet";
  return (
    Number.isFinite(height) &&
    Number.isFinite(progress) &&
    Math.abs(height) > 0 &&
    progress > 0
  );
}

export function reduceStructuredChatKeyboardLifecycleGate(
  gate: StructuredChatKeyboardLifecycleGate,
  event: StructuredChatKeyboardLifecycleEvent,
): StructuredChatKeyboardLifecycleGate {
  "worklet";
  if (event.type === "native_sample" || event.type === "composer_focus_bind") {
    if (
      !gate.enabled ||
      !gate.appActive ||
      !structuredChatKeyboardGeometryIsOpen(event.height, event.progress)
    ) {
      return gate;
    }
    if (gate.acceptedNativeSampleEpoch === gate.epoch) {
      return gate;
    }
    return {
      ...gate,
      acceptedNativeSampleEpoch: gate.epoch,
    };
  }

  const nextEnabled =
    event.type === "set_enabled" ? event.enabled : gate.enabled;
  const nextAppActive =
    event.type === "app_state" ? event.active : gate.appActive;
  if (nextEnabled === gate.enabled && nextAppActive === gate.appActive) {
    return gate;
  }

  return {
    enabled: nextEnabled,
    appActive: nextAppActive,
    epoch: gate.epoch + 1,
    acceptedNativeSampleEpoch: gate.acceptedNativeSampleEpoch,
  };
}

export function structuredChatKeyboardLifecycleGateOpen(
  gate: StructuredChatKeyboardLifecycleGate,
) {
  "worklet";
  return (
    gate.enabled &&
    gate.appActive &&
    gate.acceptedNativeSampleEpoch === gate.epoch
  );
}

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

export function structuredChatGatedOverlayTranslateY({
  gate,
  keyboardTranslation,
  keyboardProgress,
  keyboardVerticalOffset,
}: StructuredChatGatedOverlayTranslateYInput) {
  "worklet";
  if (!structuredChatKeyboardLifecycleGateOpen(gate)) {
    return 0;
  }
  return structuredChatOverlayTranslateY(
    keyboardTranslation,
    keyboardProgress,
    keyboardVerticalOffset,
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
  const opaqueBottomInset = overlayLift + height * (1 - CONTENT_FADE_START);
  const transparentBottomInset = overlayLift + height * (1 - CONTENT_FADE_END);

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
  return platform === "android" ? rawOffset - clearance : rawOffset;
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

export function structuredChatEffectiveClearanceForKeyboardLifecycle(
  input: StructuredChatEffectiveClearanceInput & {
    gate: StructuredChatKeyboardLifecycleGate;
  },
) {
  "worklet";
  // Ordinary contractions preserve the reader's occupied range. A lifecycle
  // invalidation is different: that occupied range came from an obsolete IME
  // sample and must contract with the gated Composer/fade translation.
  if (!structuredChatKeyboardLifecycleGateOpen(input.gate)) {
    return Math.max(0, input.requestedClearance);
  }
  return structuredChatEffectiveClearance(input);
}

export function structuredChatLatestOffset(
  clearance: number,
  platform: StructuredChatInsetPlatform,
) {
  "worklet";
  return platform === "ios" ? -Math.max(0, clearance) : 0;
}

export function structuredChatFocusSample(
  intentToken: number,
  clearance: number,
  platform: StructuredChatInsetPlatform,
) {
  "worklet";
  const effectiveClearance = Math.max(0, clearance);
  return {
    intentToken,
    clearance: effectiveClearance,
    latestOffset: structuredChatLatestOffset(effectiveClearance, platform),
  };
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
    composerTop: canvasHeight - effectiveKeyboardHeight - safeComposerHeight,
    scrollClearance,
    timelineHeight: canvasHeight,
    contentOffsetDelta: 0,
  };
}
