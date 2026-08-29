export const TIMELINE_BOTTOM_THRESHOLD = 96;

/** Fixed virtualization settings. Scroll position remains owned by FlatList. */
export function timelineListStabilityProps() {
  return {
    removeClippedSubviews: false,
    // Selectable Android text must not implicitly scroll the timeline.
    scrollsChildToFocus: false,
    windowSize: 5,
    maxToRenderPerBatch: 6,
    initialNumToRender: 8,
    updateCellsBatchingPeriod: 48,
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
  previousDistanceFromBottom: number = distanceFromBottom,
): TimelineScrollState {
  // FlatList also reports offsets caused by layout and content mutations. Those
  // events describe geometry, not a change in the reader's follow intent.
  if (!userDriven) {
    return state;
  }
  const distance = Math.max(0, distanceFromBottom);
  const previousDistance = Math.max(0, previousDistanceFromBottom);
  if (distance > previousDistance + 1) {
    return { mode: "detached" };
  }
  if (
    distance < previousDistance - 1 &&
    distance <= TIMELINE_BOTTOM_THRESHOLD
  ) {
    return INITIAL_TIMELINE_SCROLL_STATE;
  }
  return state;
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
