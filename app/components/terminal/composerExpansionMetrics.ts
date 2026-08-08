/**
 * Pure geometry for the shared expanding Composer capsule.
 *
 * One progress value (0 compact, 1 focused/expanded) owns every coordinated
 * motion: capsule height (via the action band), corner radius, input-region
 * horizontal padding, button translation (buttons are bottom-anchored so the
 * growing band carries them), and the quiet model-control reveal. Reduced
 * Motion resolves the same targets immediately.
 */

export const COMPOSER_INPUT_MIN_HEIGHT = 44;
export const COMPOSER_ACTION_BUTTON_SIZE = 44;
export const COMPOSER_ACTION_BAND_VERTICAL_PADDING = 6;
export const COMPOSER_COMPACT_VERTICAL_PADDING = 6;
export const COMPOSER_INPUT_HORIZONTAL_PADDING_COMPACT = {
  left: COMPOSER_ACTION_BUTTON_SIZE + 6,
  right: 74,
} as const;
export const COMPOSER_INPUT_HORIZONTAL_PADDING_EXPANDED = {
  left: 6,
  right: 8,
} as const;

export const COMPOSER_ACTION_BAND_HEIGHT =
  COMPOSER_ACTION_BUTTON_SIZE + COMPOSER_ACTION_BAND_VERTICAL_PADDING * 2;
export const COMPOSER_COMPACT_CAPSULE_HEIGHT =
  COMPOSER_INPUT_MIN_HEIGHT + COMPOSER_COMPACT_VERTICAL_PADDING * 2;
export const COMPOSER_EXPANDED_CAPSULE_BASE_HEIGHT =
  COMPOSER_COMPACT_CAPSULE_HEIGHT + COMPOSER_ACTION_BAND_HEIGHT;

export const COMPOSER_RADIUS_COMPACT = 24;
export const COMPOSER_RADIUS_EXPANDED = 18;

export const COMPOSER_MODEL_CONTROL_LEFT_INSET =
  COMPOSER_ACTION_BUTTON_SIZE + COMPOSER_ACTION_BAND_VERTICAL_PADDING + 8;
export const COMPOSER_MODEL_CONTROL_RIGHT_INSET = 82;

/**
 * The quiet model control fills the action band's 44 pt slot so the
 * accessible touch target is never smaller than the trailing Send/Stop slot.
 */
export const COMPOSER_MODEL_CONTROL_HEIGHT = 44;
export const COMPOSER_MODEL_CONTROL_LABEL_LINE_HEIGHT = 18;

/**
 * Composer text never renders beyond 2x the system font scale; the geometry
 * contract below guarantees no vertical clipping at that cap.
 */
export const COMPOSER_MAX_FONT_SCALE = 2;

/** True when a label line at the given scale fits the control without clipping. */
export function composerModelControlLabelFits(scale: number): boolean {
  return (
    COMPOSER_MODEL_CONTROL_LABEL_LINE_HEIGHT * Math.max(1, scale) <=
    COMPOSER_MODEL_CONTROL_HEIGHT
  );
}

export const COMPOSER_SPRING_CONFIG = {
  damping: 20,
  stiffness: 240,
  mass: 0.9,
  overshootClamping: true,
} as const;

/**
 * Clamp inside the UI Runtime. Keep this declaration before every exported
 * worklet that captures it: the Worklets transform initializes worklet
 * closures in source order, so a later declaration would be captured as
 * `undefined` in the generated bundle.
 */
function clampProgress(progress: number): number {
  "worklet";
  return Math.max(0, Math.min(1, Number.isFinite(progress) ? progress : 0));
}

/** The expansion band (0 compact, ACTION_BAND_HEIGHT expanded). */
export function composerActionBandHeight(progress: number): number {
  "worklet";
  return clampProgress(progress) * COMPOSER_ACTION_BAND_HEIGHT;
}

/** Capsule corner radius for a given progress. */
export function composerExpansionRadius(progress: number): number {
  "worklet";
  const p = clampProgress(progress);
  return COMPOSER_RADIUS_COMPACT + (COMPOSER_RADIUS_EXPANDED - COMPOSER_RADIUS_COMPACT) * p;
}

/** Bottom anchor of the action buttons inside the capsule. */
export function composerActionButtonBottomInset(progress: number): number {
  "worklet";
  return COMPOSER_ACTION_BAND_VERTICAL_PADDING;
}

/**
 * Input-region horizontal padding: clears the bottom-anchored Plus and
 * Send/Stop circles in the compact single row, then widens as the action band
 * opens below.
 */
export function composerInputHorizontalPadding(progress: number): {
  left: number;
  right: number;
} {
  "worklet";
  const p = clampProgress(progress);
  return {
    left:
      COMPOSER_INPUT_HORIZONTAL_PADDING_COMPACT.left +
      (COMPOSER_INPUT_HORIZONTAL_PADDING_EXPANDED.left -
        COMPOSER_INPUT_HORIZONTAL_PADDING_COMPACT.left) *
        p,
    right:
      COMPOSER_INPUT_HORIZONTAL_PADDING_COMPACT.right +
      (COMPOSER_INPUT_HORIZONTAL_PADDING_EXPANDED.right -
        COMPOSER_INPUT_HORIZONTAL_PADDING_COMPACT.right) *
        p,
  };
}

export const COMPOSER_MODEL_CONTROL_REVEAL_RANGE = [0.25, 0.85] as const;

/** Control opacity and rise for a given progress. */
export function composerModelControlReveal(progress: number): {
  opacity: number;
  translateY: number;
} {
  "worklet";
  const p = clampProgress(progress);
  const [start, end] = COMPOSER_MODEL_CONTROL_REVEAL_RANGE;
  const local = Math.max(0, Math.min(1, (p - start) / (end - start)));
  return {
    opacity: local,
    translateY: (1 - local) * 10,
  };
}

/**
 * The animation target: the capsule expands exactly when the Composer input
 * is focused.
 */
export function composerExpansionTarget(focused: boolean): 0 | 1 {
  return focused ? 1 : 0;
}

/** Reduced Motion settles the capsule immediately (no spatial animation). */
export function composerMotionDisabled(
  reducedMotion: boolean | null | undefined,
): boolean {
  return reducedMotion === true;
}
