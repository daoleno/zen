// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  INITIAL_TIMELINE_SCROLL_STATE,
  TIMELINE_LIST_STABILITY_PROPS,
  reduceTimelineScrollPosition,
  returnTimelineToBottom,
  timelineMutationDecision,
} from "./timelineScrollPolicy";

describe("timeline scroll policy", () => {
  test("append while detached preserves the visible anchor and exposes new messages", () => {
    const detached = reduceTimelineScrollPosition(
      INITIAL_TIMELINE_SCROLL_STATE,
      320,
      true,
    );

    expect(timelineMutationDecision(detached)).toBe("preserve-visible-anchor");
    expect(detached.showNewMessages).toBe(true);
  });

  test("an in-place streaming height update while detached preserves the anchor", () => {
    const detached = {
      attachedToBottom: false,
      showNewMessages: true,
    };

    expect(timelineMutationDecision(detached)).toBe("preserve-visible-anchor");
  });

  test("attached-bottom mutations follow the latest content", () => {
    expect(timelineMutationDecision(INITIAL_TIMELINE_SCROLL_STATE)).toBe(
      "follow-bottom",
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

  test("user-initiated return reattaches and clears the affordance", () => {
    expect(returnTimelineToBottom()).toEqual(INITIAL_TIMELINE_SCROLL_STATE);
  });

  test("list integration delegates pixel anchoring to native visible-child tracking", () => {
    expect(TIMELINE_LIST_STABILITY_PROPS).toEqual({
      maintainVisibleContentPosition: { minIndexForVisible: 0 },
      removeClippedSubviews: false,
    });
  });
});
