// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  captureTimelineScrollAnchor,
  resolveTimelineScrollAnchorOffset,
} from "./timelineScrollAnchor";

describe("timeline scroll anchors", () => {
  test("stores a stable message id and relative content offset", () => {
    expect(captureTimelineScrollAnchor("history", 412, 368)).toEqual({
      itemId: "history",
      relativeOffset: 44,
    });
  });

  test("restores the same relative position after row relayout", () => {
    const anchor = captureTimelineScrollAnchor("history", 412, 368);
    expect(anchor && resolveTimelineScrollAnchorOffset(anchor, 521)).toBe(565);
  });

  test("rejects invalid geometry and clamps to the scrollable range", () => {
    expect(captureTimelineScrollAnchor("", 1, 1)).toBeNull();
    expect(captureTimelineScrollAnchor("history", Number.NaN, 1)).toBeNull();
    const anchor = { itemId: "history", relativeOffset: -20 };
    expect(resolveTimelineScrollAnchorOffset(anchor, 4, 10)).toBe(0);
    expect(resolveTimelineScrollAnchorOffset(anchor, 40, 10)).toBe(10);
  });
});
