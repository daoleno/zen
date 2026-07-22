// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  INITIAL_TIMELINE_SCROLL_STATE,
  TIMELINE_LIST_STABILITY_PROPS,
  reduceTimelineScrollPosition,
  returnTimelineToBottom,
  timelineDistanceFromLatest,
  timelineMutationDecision,
} from "./timelineScrollPolicy";
import {
  createTimelineActivityExpansionState,
  reduceTimelineActivityExpansion,
  resolveTimelineActivityExpansion,
  type TimelineActivityExpansionState,
} from "./InterfaceTimelineActivityExpansionState";

type StreamingTouchOutcome = {
  feedbackShown: boolean;
  pressCancelled: boolean;
  expansionState: TimelineActivityExpansionState;
};

/**
 * Replays the native responder ordering behind a Tool-header tap. TouchableOpacity
 * first accepts the touch and shows onPressIn feedback. If the pinned timeline
 * then calls scrollToOffset for a streaming content-size mutation, ScrollView's
 * onScrollShouldSetResponder sees the still-active touch, takes the responder,
 * and Pressability receives RESPONDER_TERMINATED instead of RESPONDER_RELEASE.
 */
function releaseAcceptedToolTouchAfterMutation(
  expansionState: TimelineActivityExpansionState,
  decision: ReturnType<typeof timelineMutationDecision>,
): StreamingTouchOutcome {
  const outcome: StreamingTouchOutcome = {
    feedbackShown: true,
    pressCancelled: false,
    expansionState,
  };

  if (decision === "follow-bottom") {
    outcome.pressCancelled = true;
  }
  if (!outcome.pressCancelled) {
    outcome.expansionState = reduceTimelineActivityExpansion(expansionState, {
      eventId: expansionState.eventId,
      defaultExpanded: false,
    });
  }
  return outcome;
}

describe("timeline scroll policy", () => {
  test("append while detached preserves the visible anchor and exposes new messages", () => {
    const detached = reduceTimelineScrollPosition(
      INITIAL_TIMELINE_SCROLL_STATE,
      320,
      true,
    );

    expect(timelineMutationDecision(detached)).toBe("preserve-visible-anchor");
    expect(detached.mode).toBe("detached");
  });

  test("an in-place streaming height update while detached preserves the anchor", () => {
    const detached = {
      mode: "detached" as const,
    };

    expect(timelineMutationDecision(detached)).toBe("preserve-visible-anchor");
  });

  test("attached-bottom mutations follow the latest content", () => {
    expect(timelineMutationDecision(INITIAL_TIMELINE_SCROLL_STATE)).toBe(
      "follow-bottom",
    );
  });

  test("same-event streaming cannot cancel an accepted Tool-header touch", () => {
    const decision = timelineMutationDecision(
      INITIAL_TIMELINE_SCROLL_STATE,
      true,
    );
    let expansionState = createTimelineActivityExpansionState("tool-stream");

    const opened = releaseAcceptedToolTouchAfterMutation(
      expansionState,
      decision,
    );
    expect(opened).toMatchObject({
      feedbackShown: true,
      pressCancelled: false,
    });
    expect(
      resolveTimelineActivityExpansion(
        opened.expansionState,
        "tool-stream",
        false,
      ),
    ).toBe(true);
    expect(decision).toBe("suspend-implicit-anchor");

    expansionState = opened.expansionState;
    const closed = releaseAcceptedToolTouchAfterMutation(
      expansionState,
      decision,
    );
    expect(closed.pressCancelled).toBe(false);
    expect(
      resolveTimelineActivityExpansion(
        closed.expansionState,
        "tool-stream",
        false,
      ),
    ).toBe(false);

    expect(timelineMutationDecision(INITIAL_TIMELINE_SCROLL_STATE, false)).toBe(
      "follow-bottom",
    );
  });

  test("touch suspension preserves detached reader scroll intent", () => {
    const detached = { mode: "detached" as const };

    expect(timelineMutationDecision(detached, true)).toBe(
      "suspend-implicit-anchor",
    );
    expect(timelineMutationDecision(detached, false)).toBe(
      "preserve-visible-anchor",
    );
  });

  test("only user movement beyond the threshold detaches", () => {
    expect(
      reduceTimelineScrollPosition(INITIAL_TIMELINE_SCROLL_STATE, 320, false),
    ).toEqual(INITIAL_TIMELINE_SCROLL_STATE);
    expect(
      reduceTimelineScrollPosition(INITIAL_TIMELINE_SCROLL_STATE, 48, true),
    ).toEqual(INITIAL_TIMELINE_SCROLL_STATE);
  });

  test("layout movement cannot override detached user intent near the latest content", () => {
    const detached = { mode: "detached" as const };

    expect(reduceTimelineScrollPosition(detached, 24, false)).toBe(detached);
    expect(timelineMutationDecision(detached)).toBe("preserve-visible-anchor");
  });

  test("layout movement cannot detach an attached streaming viewport", () => {
    expect(
      reduceTimelineScrollPosition(INITIAL_TIMELINE_SCROLL_STATE, 320, false),
    ).toEqual(INITIAL_TIMELINE_SCROLL_STATE);
  });

  test("user movement reattaches only after returning within the latest threshold", () => {
    const detached = { mode: "detached" as const };

    expect(reduceTimelineScrollPosition(detached, 97, true)).toEqual(detached);
    expect(reduceTimelineScrollPosition(detached, 96, true)).toEqual(
      INITIAL_TIMELINE_SCROLL_STATE,
    );
  });

  test("user-initiated return reattaches and clears the affordance", () => {
    expect(returnTimelineToBottom()).toEqual(INITIAL_TIMELINE_SCROLL_STATE);
  });

  test("measures distance from an inset-aware latest target", () => {
    expect(timelineDistanceFromLatest(-356, -356)).toBe(0);
    expect(timelineDistanceFromLatest(0, -356)).toBe(356);
    expect(timelineDistanceFromLatest(280, 0)).toBe(280);
  });

  test("list integration delegates pixel anchoring to native visible-child tracking", () => {
    expect(TIMELINE_LIST_STABILITY_PROPS).toEqual({
      maintainVisibleContentPosition: { minIndexForVisible: 0 },
      removeClippedSubviews: false,
      scrollsChildToFocus: false,
    });
  });

  test("native child focus cannot become a second timeline scroll owner", () => {
    expect(TIMELINE_LIST_STABILITY_PROPS.scrollsChildToFocus).toBe(false);
  });
});
