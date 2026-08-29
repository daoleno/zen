// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  INITIAL_TIMELINE_SCROLL_STATE,
  TIMELINE_BOTTOM_THRESHOLD,
  reduceTimelineScrollPosition,
  returnTimelineToBottom,
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


  test("user-initiated return reattaches and clears the affordance", () => {
    expect(returnTimelineToBottom()).toEqual(INITIAL_TIMELINE_SCROLL_STATE);
  });

  test("measures distance from an inset-aware latest target", () => {
    expect(timelineDistanceFromLatest(-356, -356)).toBe(0);
    expect(timelineDistanceFromLatest(0, -356)).toBe(356);
    expect(timelineDistanceFromLatest(280, 0)).toBe(280);
  });

  test("list integration leaves position ownership to the native list", () => {
    expect(timelineListStabilityProps()).toEqual({
      removeClippedSubviews: false,
      scrollsChildToFocus: false,
      windowSize: 5,
      maxToRenderPerBatch: 6,
      initialNumToRender: 8,
      updateCellsBatchingPeriod: 48,
    });
  });

  test("native child focus cannot become a second timeline scroll owner", () => {
    expect(timelineListStabilityProps().scrollsChildToFocus).toBe(false);
  });

  test("list virtualization remains fixed across interaction state", () => {
    expect(timelineListStabilityProps()).not.toHaveProperty(
      "maintainVisibleContentPosition",
    );
    expect(timelineListStabilityProps()).not.toHaveProperty(
      "disableVirtualization",
    );
  });

  test("a fling keeps native follow suspended across drag-end to momentum-begin", () => {
    expect(timelineDragContinuesWithMomentum(1.2)).toBe(true);
    expect(timelineDragContinuesWithMomentum(-0.4)).toBe(true);
    expect(timelineDragContinuesWithMomentum(0)).toBe(false);
    expect(timelineDragContinuesWithMomentum(undefined)).toBe(false);
  });
});
