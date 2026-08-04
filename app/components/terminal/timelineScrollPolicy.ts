export const TIMELINE_BOTTOM_THRESHOLD = 96;
const TIMELINE_NATIVE_ANCHOR = { minIndexForVisible: 0 } as const;

export function timelineListStabilityProps(followSuspended: boolean) {
  return {
    maintainVisibleContentPosition: followSuspended
      ? TIMELINE_NATIVE_ANCHOR
      : {
          ...TIMELINE_NATIVE_ANCHOR,
          autoscrollToTopThreshold: TIMELINE_BOTTOM_THRESHOLD,
        },
    removeClippedSubviews: false,
    // Selectable Android text takes native focus. Timeline policy, rather than
    // descendant focus, owns any viewport movement in this inverted list.
    scrollsChildToFocus: false,
  } as const;
}

export interface TimelineScrollState {
  mode: "attached" | "detached";
}

export const INITIAL_TIMELINE_SCROLL_STATE: TimelineScrollState = {
  mode: "attached",
};

export function reduceTimelineScrollPosition(
  state: TimelineScrollState,
  distanceFromBottom: number,
  userDriven: boolean,
): TimelineScrollState {
  // FlatList also reports offsets caused by layout and content mutations. Those
  // events describe geometry, not a change in the reader's follow intent.
  if (!userDriven) {
    return state;
  }
  return Math.max(0, distanceFromBottom) <= TIMELINE_BOTTOM_THRESHOLD
    ? INITIAL_TIMELINE_SCROLL_STATE
    : { mode: "detached" };
}

export function returnTimelineToBottom(): TimelineScrollState {
  return INITIAL_TIMELINE_SCROLL_STATE;
}

export function timelineDistanceFromLatest(
  contentOffset: number,
  latestOffset: number,
) {
  return Math.max(0, contentOffset - latestOffset);
}

export function timelineDragContinuesWithMomentum(velocity?: number) {
  return Number.isFinite(velocity) && Math.abs(velocity ?? 0) > 0.01;
}
