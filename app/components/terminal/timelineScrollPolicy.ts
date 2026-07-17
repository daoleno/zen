export const TIMELINE_BOTTOM_THRESHOLD = 96;
export const TIMELINE_LIST_STABILITY_PROPS = {
  maintainVisibleContentPosition: { minIndexForVisible: 0 },
  removeClippedSubviews: false,
} as const;

export interface TimelineScrollState {
  mode: "attached" | "detached";
}

export type TimelineMutationDecision =
  | "follow-bottom"
  | "preserve-visible-anchor"
  | "suspend-implicit-anchor";

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

export function timelineMutationDecision(
  state: TimelineScrollState,
  implicitAnchorSuspended: boolean = false,
): TimelineMutationDecision {
  if (implicitAnchorSuspended) {
    return "suspend-implicit-anchor";
  }
  return state.mode === "attached"
    ? "follow-bottom"
    : "preserve-visible-anchor";
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
