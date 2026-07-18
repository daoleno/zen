import { describe, expect, test } from "bun:test";
import {
  TOOL_DISCLOSURE_TOUCH_SLOP_PX,
  toolDisclosureMovedBeyondSlop,
  toolDisclosureShouldCommitToggle,
} from "./timelineDisclosurePress";

describe("tool disclosure press policy", () => {
  test("touch slop retains ownership until movement exceeds 10 px", () => {
    expect(TOOL_DISCLOSURE_TOUCH_SLOP_PX).toBe(10);
    expect(toolDisclosureMovedBeyondSlop(0, 0, 0, 10)).toBe(false);
    expect(toolDisclosureMovedBeyondSlop(0, 0, 10, 0)).toBe(false);
    expect(toolDisclosureMovedBeyondSlop(0, 0, 0, 11)).toBe(true);
    expect(toolDisclosureMovedBeyondSlop(5, 5, 16, 5)).toBe(true);
  });

  test("clean release commits exactly one expandable toggle", () => {
    expect(
      toolDisclosureShouldCommitToggle({
        canExpand: true,
        gestureActive: true,
        userMovedBeyondSlop: false,
      }),
    ).toBe(true);
  });

  test("drag beyond slop never toggles on release", () => {
    expect(
      toolDisclosureShouldCommitToggle({
        canExpand: true,
        gestureActive: true,
        userMovedBeyondSlop: true,
      }),
    ).toBe(false);
  });

  test("non-expandable and inactive gestures never toggle", () => {
    expect(
      toolDisclosureShouldCommitToggle({
        canExpand: false,
        gestureActive: true,
        userMovedBeyondSlop: false,
      }),
    ).toBe(false);
    expect(
      toolDisclosureShouldCommitToggle({
        canExpand: true,
        gestureActive: false,
        userMovedBeyondSlop: false,
      }),
    ).toBe(false);
  });
});
