// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  INITIAL_TIMELINE_SCROLL_STATE,
  TIMELINE_BOTTOM_THRESHOLD,
  focusTimelineOnSentMessage,
  reduceTimelineScrollPosition,
  returnTimelineToBottom,
  settleFocusedTimeline,
  shouldFocusTimelineOnSentMessage,
  timelineDragContinuesWithMomentum,
  timelineListStabilityProps,
  timelineDistanceFromLatest,
} from "./timelineScrollPolicy";

describe("timeline scroll policy", () => {
  test("layout movement never changes follow intent", () => {
    expect(
      reduceTimelineScrollPosition(INITIAL_TIMELINE_SCROLL_STATE, 320, false),
    ).toEqual(INITIAL_TIMELINE_SCROLL_STATE);
  });

  test("any user movement away from latest detaches immediately", () => {
    expect(
      reduceTimelineScrollPosition(
        INITIAL_TIMELINE_SCROLL_STATE,
        12,
        true,
        0,
      ),
    ).toEqual({ mode: "detached" });
  });

  test("layout movement cannot override detached user intent near the latest content", () => {
    const detached = { mode: "detached" as const };

    expect(reduceTimelineScrollPosition(detached, 24, false)).toBe(detached);
  });

  test("layout movement cannot detach an attached streaming viewport", () => {
    expect(
      reduceTimelineScrollPosition(INITIAL_TIMELINE_SCROLL_STATE, 320, false),
    ).toEqual(INITIAL_TIMELINE_SCROLL_STATE);
  });

  test("user movement reattaches only after returning within the latest threshold", () => {
    const detached = { mode: "detached" as const };

    expect(reduceTimelineScrollPosition(detached, 97, true, 160)).toEqual(
      detached,
    );
    expect(reduceTimelineScrollPosition(detached, 96, true, 160)).toEqual(
      INITIAL_TIMELINE_SCROLL_STATE,
    );
  });

  test("a sent-message focus settles detached until the reader follows again", () => {
    const focused = focusTimelineOnSentMessage();

    expect(focused).toEqual({ mode: "focused" });
    expect(settleFocusedTimeline(focused)).toEqual({ mode: "detached" });
    expect(settleFocusedTimeline(INITIAL_TIMELINE_SCROLL_STATE)).toBe(
      INITIAL_TIMELINE_SCROLL_STATE,
    );
  });

  test("sending while reading history does not enter turn-focus", () => {
    expect(shouldFocusTimelineOnSentMessage({ mode: "attached" })).toBe(true);
    expect(shouldFocusTimelineOnSentMessage({ mode: "detached" })).toBe(false);
    expect(shouldFocusTimelineOnSentMessage({ mode: "focused" })).toBe(false);
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
    expect(timelineListStabilityProps(false)).toEqual({
      maintainVisibleContentPosition: {
        minIndexForVisible: 0,
        autoscrollToTopThreshold: TIMELINE_BOTTOM_THRESHOLD,
      },
      removeClippedSubviews: false,
      scrollsChildToFocus: false,
      windowSize: 5,
      maxToRenderPerBatch: 6,
      initialNumToRender: 8,
      updateCellsBatchingPeriod: 48,
    });
  });

  test("native child focus cannot become a second timeline scroll owner", () => {
    expect(timelineListStabilityProps(false).scrollsChildToFocus).toBe(false);
  });

  test("touch and text selection suspend native follow without disabling native anchoring", () => {
    expect(timelineListStabilityProps(true)).toMatchObject({
      maintainVisibleContentPosition: { minIndexForVisible: 0 },
      removeClippedSubviews: false,
      scrollsChildToFocus: false,
      // Detached reading widens the bounded viewport-multiple window so rows
      // near the reader stay measured through newest-edge mutations.
      windowSize: 21,
      maxToRenderPerBatch: 24,
      initialNumToRender: 16,
    });
    expect(timelineListStabilityProps(true)).not.toHaveProperty(
      "disableVirtualization",
    );
    expect(
      timelineListStabilityProps(true).maintainVisibleContentPosition,
    ).not.toHaveProperty("autoscrollToTopThreshold");
    expect(
      timelineListStabilityProps(false).maintainVisibleContentPosition,
    ).toEqual({
      minIndexForVisible: 0,
      autoscrollToTopThreshold: TIMELINE_BOTTOM_THRESHOLD,
    });
  });

  test("a fling keeps native follow suspended across drag-end to momentum-begin", () => {
    expect(timelineDragContinuesWithMomentum(1.2)).toBe(true);
    expect(timelineDragContinuesWithMomentum(-0.4)).toBe(true);
    expect(timelineDragContinuesWithMomentum(0)).toBe(false);
    expect(timelineDragContinuesWithMomentum(undefined)).toBe(false);
  });
});
