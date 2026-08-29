export const TIMELINE_BOTTOM_THRESHOLD = 96;
const TIMELINE_NATIVE_ANCHOR = { minIndexForVisible: 0 } as const;

/**
 * Bounded virtualization for the chat timeline. Both modes keep a fixed
 * viewport-multiple render window (never proportional to history length):
 * attached (following latest) keeps the established tight window for stream
 * cost; detached reading widens the window so rows near the reader stay
 * measured while newest-edge append / same-ID stream / Work insertion update.
 *
 * The native anchor is the sole position owner (Android
 * MaintainVisibleScrollPositionHelper pins the first visible cell across
 * UIManager mounts; iOS anchors by cell index). Mounting the entire history
 * (initialNumToRender/maxToRenderPerBatch = data length) is forbidden: it
 * disables virtualization and cannot establish a production fix.
 */
export const TIMELINE_ATTACHED_WINDOW_SIZE = 5;
export const TIMELINE_DETACHED_WINDOW_SIZE = 21;
export const TIMELINE_ATTACHED_MAX_TO_RENDER_PER_BATCH = 6;
export const TIMELINE_DETACHED_MAX_TO_RENDER_PER_BATCH = 24;
export const TIMELINE_ATTACHED_INITIAL_NUM_TO_RENDER = 8;
export const TIMELINE_DETACHED_INITIAL_NUM_TO_RENDER = 16;

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
    windowSize: followSuspended
      ? TIMELINE_DETACHED_WINDOW_SIZE
      : TIMELINE_ATTACHED_WINDOW_SIZE,
    maxToRenderPerBatch: followSuspended
      ? TIMELINE_DETACHED_MAX_TO_RENDER_PER_BATCH
      : TIMELINE_ATTACHED_MAX_TO_RENDER_PER_BATCH,
    initialNumToRender: followSuspended
      ? TIMELINE_DETACHED_INITIAL_NUM_TO_RENDER
      : TIMELINE_ATTACHED_INITIAL_NUM_TO_RENDER,
    updateCellsBatchingPeriod: 48,
  } as const;
}

export interface TimelineScrollState {
  mode: "attached" | "focused" | "detached";
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

export function focusTimelineOnSentMessage(): TimelineScrollState {
  return { mode: "focused" };
}

export function shouldFocusTimelineOnSentMessage(state: TimelineScrollState) {
  return state.mode === "attached";
}

export function settleFocusedTimeline(
  state: TimelineScrollState,
): TimelineScrollState {
  return state.mode === "focused" ? { mode: "detached" } : state;
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
