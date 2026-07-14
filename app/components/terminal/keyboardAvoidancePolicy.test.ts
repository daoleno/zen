// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { shouldAvoidKeyboard } from "./keyboardAvoidancePolicy";
import {
  INITIAL_TIMELINE_SCROLL_STATE,
  reduceTimelineScrollPosition,
  timelineMutationDecision,
} from "./timelineScrollPolicy";

describe("chat keyboard avoidance policy", () => {
  test("open to closed always releases the active surface", () => {
    expect(shouldAvoidKeyboard(true, true)).toBe(true);
    expect(shouldAvoidKeyboard(true, false)).toBe(false);
  });

  test("inactive surfaces never retain keyboard avoidance", () => {
    expect(shouldAvoidKeyboard(false, true)).toBe(false);
    expect(shouldAvoidKeyboard(false, false)).toBe(false);
  });

  test("closing the keyboard preserves detached history intent", () => {
    const detached = reduceTimelineScrollPosition(
      INITIAL_TIMELINE_SCROLL_STATE,
      320,
      true,
    );

    expect(shouldAvoidKeyboard(true, true)).toBe(true);
    expect(shouldAvoidKeyboard(true, false)).toBe(false);
    expect(timelineMutationDecision(detached)).toBe("preserve-visible-anchor");
  });

  test("closing the keyboard preserves attached streaming intent", () => {
    expect(shouldAvoidKeyboard(true, true)).toBe(true);
    expect(shouldAvoidKeyboard(true, false)).toBe(false);
    expect(timelineMutationDecision(INITIAL_TIMELINE_SCROLL_STATE)).toBe(
      "follow-bottom",
    );
  });
});
