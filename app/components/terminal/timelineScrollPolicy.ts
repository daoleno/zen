export const TIMELINE_BOTTOM_THRESHOLD = 96;
export const TIMELINE_LIST_STABILITY_PROPS = {
  maintainVisibleContentPosition: { minIndexForVisible: 0 },
  removeClippedSubviews: false,
} as const;

export interface TimelineScrollState {
  attachedToBottom: boolean;
  showNewMessages: boolean;
}

export type TimelineMutationDecision = "follow-bottom" | "preserve-visible-anchor";

export const INITIAL_TIMELINE_SCROLL_STATE: TimelineScrollState = {
  attachedToBottom: true,
  showNewMessages: false,
};

export function reduceTimelineScrollPosition(
  state: TimelineScrollState,
  distanceFromBottom: number,
  userDriven: boolean,
): TimelineScrollState {
  if (Math.max(0, distanceFromBottom) <= TIMELINE_BOTTOM_THRESHOLD) {
    return INITIAL_TIMELINE_SCROLL_STATE;
  }
  if (!userDriven) {
    return state;
  }
  return {
    attachedToBottom: false,
    showNewMessages: true,
  };
}

export function timelineMutationDecision(
  state: TimelineScrollState,
): TimelineMutationDecision {
  return state.attachedToBottom ? "follow-bottom" : "preserve-visible-anchor";
}

export function returnTimelineToBottom(): TimelineScrollState {
  return INITIAL_TIMELINE_SCROLL_STATE;
}
