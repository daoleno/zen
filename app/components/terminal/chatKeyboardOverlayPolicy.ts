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
  composerFocused: boolean;
  revision: number;
  authoritativeRevision: number;
  nativeImeVisible: boolean;
  nativeComposerFocused: boolean;
  forceLifecycleContractionRevision: number;
  keyboardTranslation: number;
  keyboardProgress: number;
}

export type StructuredChatKeyboardLifecycleEvent =
  | { type: "set_enabled"; enabled: boolean }
  | { type: "app_state"; active: boolean }
  | { type: "composer_focus"; focused: boolean }
  | {
      type: "native_sample";
      sourceRevision: number;
      height: number;
      progress: number;
      updatesGeometry: boolean;
      /** The native handler delivered the terminal animation sample. */
      settled?: boolean;
    }
  | {
      type: "authoritative_snapshot";
      sourceRevision: number;
      imeVisible: boolean;
      imeHeight: number;
      composerFocused: boolean;
      foregroundReconciliation: boolean;
    };

export interface StructuredChatGatedOverlayTranslateYInput {
  gate: StructuredChatKeyboardLifecycleGate;
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
    composerFocused: false,
    revision: 1,
    authoritativeRevision: 0,
    nativeImeVisible: false,
    nativeComposerFocused: false,
    forceLifecycleContractionRevision: 0,
    keyboardTranslation: 0,
    keyboardProgress: 0,
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

function invalidateStructuredChatKeyboardLifecycleGate(
  gate: StructuredChatKeyboardLifecycleGate,
  changes: Partial<
    Pick<
      StructuredChatKeyboardLifecycleGate,
      "enabled" | "appActive" | "composerFocused"
    >
  >,
): StructuredChatKeyboardLifecycleGate {
  "worklet";
  return {
    ...gate,
    ...changes,
    revision: gate.revision + 1,
    authoritativeRevision: 0,
    nativeImeVisible: false,
    nativeComposerFocused: false,
    forceLifecycleContractionRevision: gate.revision + 1,
    keyboardTranslation: 0,
    keyboardProgress: 0,
  };
}

export function reduceStructuredChatKeyboardLifecycleGate(
  gate: StructuredChatKeyboardLifecycleGate,
  event: StructuredChatKeyboardLifecycleEvent,
): StructuredChatKeyboardLifecycleGate {
  "worklet";
  if (event.type === "native_sample") {
    if (
      !gate.enabled ||
      !gate.appActive ||
      event.sourceRevision !== gate.revision ||
      gate.authoritativeRevision !== gate.revision ||
      !gate.nativeImeVisible ||
      !gate.nativeComposerFocused ||
      !Number.isFinite(event.height) ||
      !Number.isFinite(event.progress)
    ) {
      return gate;
    }
    // Android can report the last non-zero IME height on the hide end
    // callback while progress is already zero. Treat that terminal sample as
    // authoritative for the overlay position so a stale keyboard height can
    // never leave the Composer floating after the IME has disappeared.
    if (
      event.settled &&
      !structuredChatKeyboardGeometryIsOpen(event.height, event.progress)
    ) {
      return {
        ...gate,
        nativeImeVisible: false,
        nativeComposerFocused: false,
        keyboardTranslation: 0,
        keyboardProgress: 0,
      };
    }
    return {
      ...gate,
      keyboardTranslation: event.updatesGeometry
        ? -Math.abs(event.height)
        : gate.keyboardTranslation,
      keyboardProgress: event.updatesGeometry
        ? event.progress
        : gate.keyboardProgress,
    };
  }

  if (event.type === "authoritative_snapshot") {
    if (
      !gate.enabled ||
      !gate.appActive ||
      event.sourceRevision !== gate.revision
    ) {
      return gate;
    }
    const nativeOpen =
      event.imeVisible &&
      event.composerFocused &&
      Number.isFinite(event.imeHeight) &&
      event.imeHeight > 0;
    return {
      ...gate,
      composerFocused:
        event.foregroundReconciliation && !nativeOpen
          ? false
          : gate.composerFocused,
      authoritativeRevision: gate.revision,
      nativeImeVisible:
        event.imeVisible &&
        Number.isFinite(event.imeHeight) &&
        event.imeHeight > 0,
      nativeComposerFocused: event.composerFocused,
      forceLifecycleContractionRevision: nativeOpen
        ? 0
        : event.foregroundReconciliation
          ? gate.revision
          : gate.forceLifecycleContractionRevision,
      keyboardTranslation: nativeOpen ? -Math.abs(event.imeHeight) : 0,
      keyboardProgress: nativeOpen ? 1 : 0,
    };
  }

  if (event.type === "composer_focus") {
    if (event.focused === gate.composerFocused) {
      return gate;
    }
    if (event.focused) {
      if (!gate.enabled || !gate.appActive) {
        return gate;
      }
      return {
        ...gate,
        composerFocused: true,
      };
    }
    if (
      gate.authoritativeRevision === gate.revision &&
      !gate.nativeImeVisible
    ) {
      return { ...gate, composerFocused: false };
    }
    return invalidateStructuredChatKeyboardLifecycleGate(gate, {
      composerFocused: false,
    });
  }

  const nextEnabled =
    event.type === "set_enabled" ? event.enabled : gate.enabled;
  const nextAppActive =
    event.type === "app_state" ? event.active : gate.appActive;
  if (nextEnabled === gate.enabled && nextAppActive === gate.appActive) {
    return gate;
  }

  return invalidateStructuredChatKeyboardLifecycleGate(gate, {
    enabled: nextEnabled,
    appActive: nextAppActive,
    composerFocused: false,
  });
}

export function structuredChatKeyboardLifecycleGateOpen(
  gate: StructuredChatKeyboardLifecycleGate,
) {
  "worklet";
  return (
    gate.enabled &&
    gate.appActive &&
    gate.composerFocused &&
    gate.authoritativeRevision === gate.revision &&
    gate.nativeImeVisible &&
    gate.nativeComposerFocused
  );
}

export interface StructuredChatKeyboardLifecycleDispatchResult {
  revision: number;
  revisionChanged: boolean;
  invalidateReason: "route" | "app" | null;
}

export interface StructuredChatAuthoritativeSnapshotDispatchResult {
  revision: number;
  accepted: boolean;
  shouldBlurComposer: boolean;
}

export type StructuredChatKeyboardLifecycleDispatchEvent = Extract<
  StructuredChatKeyboardLifecycleEvent,
  { type: "set_enabled" } | { type: "app_state" } | { type: "composer_focus" }
>;

/**
 * Single-owner gate mutation for the UI runtime. The JS thread must never
 * read-modify-write the gate shared value: its copy lags the UI runtime's
 * authoritative native snapshot and animation geometry, so a JS write-back
 * can drop the admitted revision and pin the Composer to the screen bottom.
 * JS effects schedule this worklet with runOnUIAsync and use the reported
 * transition for callback-revision and invalidation
 * bookkeeping.
 */
export function dispatchStructuredChatKeyboardLifecycleEvent(
  gate: { value: StructuredChatKeyboardLifecycleGate },
  event: StructuredChatKeyboardLifecycleDispatchEvent,
): StructuredChatKeyboardLifecycleDispatchResult {
  "worklet";
  const previous = gate.value;
  const next = reduceStructuredChatKeyboardLifecycleGate(previous, event);
  gate.value = next;
  const revisionChanged = next.revision !== previous.revision;
  const invalidateReason =
    revisionChanged && event.type === "set_enabled" && !event.enabled
      ? "route"
      : revisionChanged && event.type === "app_state" && !event.active
        ? "app"
        : null;
  return { revision: next.revision, revisionChanged, invalidateReason };
}

export function dispatchStructuredChatAuthoritativeSnapshot(
  gate: { value: StructuredChatKeyboardLifecycleGate },
  event: Extract<
    StructuredChatKeyboardLifecycleEvent,
    { type: "authoritative_snapshot" }
  >,
): StructuredChatAuthoritativeSnapshotDispatchResult {
  "worklet";
  const previous = gate.value;
  const next = reduceStructuredChatKeyboardLifecycleGate(previous, event);
  gate.value = next;
  const accepted = next !== previous;
  return {
    revision: next.revision,
    accepted,
    shouldBlurComposer:
      accepted &&
      ((!next.nativeImeVisible && previous.nativeImeVisible) ||
        (event.foregroundReconciliation &&
          (!next.nativeImeVisible || !next.nativeComposerFocused))),
  };
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
  keyboardVerticalOffset,
}: StructuredChatGatedOverlayTranslateYInput) {
  "worklet";
  if (!structuredChatKeyboardLifecycleGateOpen(gate)) {
    return 0;
  }
  return structuredChatOverlayTranslateY(
    gate.keyboardTranslation,
    gate.keyboardProgress,
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
  if (input.gate.forceLifecycleContractionRevision === input.gate.revision) {
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
