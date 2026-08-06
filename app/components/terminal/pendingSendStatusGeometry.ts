import { INTERFACE_TIMELINE_HORIZONTAL_INSET } from "./interfaceTimelineGeometry";

/**
 * Geometry for the outbound Pending send clock.
 *
 * The clock is an absolute child of the existing user bubble View and paints in
 * the timeline's horizontal inset (14), outside the bubble background bounds.
 * Non-pending messages keep the pre-change single-bubble native tree; only
 * Pending mounts the mark.
 *
 * Anchor: right = -(mark + gap) so the 11px mark sits 2px past the bubble edge
 * and still fits inside the 14px screen inset (mark+gap <= inset).
 *
 * Hand rest angles follow Telegram MsgClockDrawable: fast hand starts vertical
 * (12 o'clock), short hand starts horizontal-right (3 o'clock); slow period is
 * 3× the fast period. Reduced motion keeps both rests so the glyph stays a clock.
 */
export const PENDING_SEND_STATUS_MARK_SIZE = 11;
/** Gap between the bubble's trailing edge and the clock. */
export const PENDING_SEND_STATUS_GAP = 2;
/** Horizontal extent past the bubble edge occupied by gap + mark. */
export const PENDING_SEND_STATUS_OUTSIDE_EXTENT =
  PENDING_SEND_STATUS_MARK_SIZE + PENDING_SEND_STATUS_GAP;
/** Absolute `right` for the mark (negative = outside bubble toward screen edge). */
export const PENDING_SEND_STATUS_OUTSIDE_RIGHT =
  -PENDING_SEND_STATUS_OUTSIDE_EXTENT;

/** Fast (long) hand full rotation period — MsgClockDrawable rotateTime. */
export const PENDING_SEND_CLOCK_FAST_PERIOD_MS = 1500;
/** Slow (short) hand period is 3× the fast period. */
export const PENDING_SEND_CLOCK_SLOW_PERIOD_MS =
  PENDING_SEND_CLOCK_FAST_PERIOD_MS * 3;
/**
 * Rest rotation for a hand drawn toward 12 o'clock.
 * Fast hand rests here (vertical); slow hand rests at +90° (horizontal-right).
 */
export const PENDING_SEND_CLOCK_FAST_HAND_REST_DEG = 0;
export const PENDING_SEND_CLOCK_SLOW_HAND_REST_DEG = 90;
/** Stroke width of each hand bar (MsgClockDrawable ~1dp). */
export const PENDING_SEND_CLOCK_HAND_STROKE = 1.15;

export function pendingSendStatusFitsTimelineInset(
  extent: number = PENDING_SEND_STATUS_OUTSIDE_EXTENT,
  inset: number = INTERFACE_TIMELINE_HORIZONTAL_INSET,
): boolean {
  return extent > 0 && extent <= inset;
}
