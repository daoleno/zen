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
      reduceTimelineScrollPosition(
        INITIAL_TIMELINE_SCROLL_STATE,
        320,
        false,
      ),
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
    });
  });
});
