/**
 * Tool-card disclosure press ownership.
 *
 * React Native's TouchableOpacity is cancelable by a parent ScrollView. While a
 * finger is down on a Tool header, content-size mutations (streaming updates
 * and inset/clearance geometry) can make the parent take the responder after
 * onPressIn feedback and before onPress. The user sees press feedback with no
 * disclosure toggle.
 *
 * Invariant: an accepted Tool-header press commits exactly one disclosure
 * toggle on clean release. The parent may steal the responder only after the
 * finger has moved beyond touch slop (a real scroll). Content-driven scroll
 * without user movement cannot cancel the press. Termination resets gesture
 * state and never commits.
 */

export const TOOL_DISCLOSURE_TOUCH_SLOP_PX = 10;

export function toolDisclosureMovedBeyondSlop(
  startX: number,
  startY: number,
  pageX: number,
  pageY: number,
  slop: number = TOOL_DISCLOSURE_TOUCH_SLOP_PX,
): boolean {
  const dx = Math.abs(pageX - startX);
  const dy = Math.abs(pageY - startY);
  return dx > slop || dy > slop;
}

/** Commit only on clean release: expandable, active, and not dragged past slop. */
export function toolDisclosureShouldCommitToggle(input: {
  canExpand: boolean;
  gestureActive: boolean;
  userMovedBeyondSlop: boolean;
}): boolean {
  return (
    input.canExpand && input.gestureActive && !input.userMovedBeyondSlop
  );
}
