export const QUIET_MODES = [
  { key: "meditation" },
  { key: "mokugyo" },
  { key: "window" },
] as const;

export type QuietModeKey = (typeof QUIET_MODES)[number]["key"];

/**
 * Immersive but bounded clip length for production autoplay cycles.
 * Longer source masters are trimmed at encode time; this is the logical cap.
 */
export const WINDOW_CLIP_MAX_DURATION_MS = 12_000;

/**
 * Test-only seam: when non-null, the window player may treat a clip as ended
 * after this many ms. Production UI never sets this.
 */
let windowClipEndOverrideMs: number | null = null;

export function __getWindowClipEndOverrideMsForTests(): number | null {
  return windowClipEndOverrideMs;
}

export function __setWindowClipEndOverrideMsForTests(ms: number | null): void {
  windowClipEndOverrideMs = ms;
}

/** Effective clip end: test override, else immersive max, else natural end. */
export function resolveWindowClipEndMs(
  naturalDurationMs: number | null | undefined,
): number {
  const override = windowClipEndOverrideMs;
  if (override != null && override > 0) {
    return override;
  }
  if (
    naturalDurationMs != null &&
    naturalDurationMs > 0 &&
    naturalDurationMs < WINDOW_CLIP_MAX_DURATION_MS
  ) {
    return naturalDurationMs;
  }
  return WINDOW_CLIP_MAX_DURATION_MS;
}
