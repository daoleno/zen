import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { keyboardAvoidancePolicy } from "./keyboardAvoidancePolicy";
import {
  INITIAL_TIMELINE_SCROLL_STATE,
  reduceTimelineScrollPosition,
  timelineMutationDecision,
} from "./timelineScrollPolicy";

describe("chat keyboard avoidance policy", () => {
  test("frame has no keyboard-state or imperative style reset path", () => {
    const frame = readFileSync(
      join(import.meta.dir, "CodexChatKeyboardFrame.tsx"),
      "utf8",
    );

    expect(frame).toContain("keyboardAvoidancePolicy(enabled)");
    expect(frame).not.toContain("useKeyboardState");
    expect(frame).not.toContain("setNativeProps");
    expect(frame).not.toContain('behavior={Platform.OS === "android" ? "height"');
  });

  test("keeps one declarative layout owner through hide and second focus", () => {
    const sequence: string[] = [
      "focus",
      "type",
      "keyboard-hide",
      "refocus",
      "keyboard-show",
      "backspace",
    ];
    const policies = sequence.map(() => keyboardAvoidancePolicy(true));

    expect(policies).toEqual(
      sequence.map(() => ({ enabled: true, behavior: "padding" })),
    );
  });

  test("inactive surfaces keep the same padding implementation disabled", () => {
    expect(keyboardAvoidancePolicy(false)).toEqual({
      enabled: false,
      behavior: "padding",
    });
  });

  test("keyboard cycles preserve detached history intent", () => {
    const detached = reduceTimelineScrollPosition(
      INITIAL_TIMELINE_SCROLL_STATE,
      320,
      true,
    );

    expect(keyboardAvoidancePolicy(true)).toEqual({
      enabled: true,
      behavior: "padding",
    });
    expect(timelineMutationDecision(detached)).toBe("preserve-visible-anchor");
  });

  test("keyboard cycles preserve attached streaming intent", () => {
    expect(keyboardAvoidancePolicy(true)).toEqual({
      enabled: true,
      behavior: "padding",
    });
    expect(timelineMutationDecision(INITIAL_TIMELINE_SCROLL_STATE)).toBe(
      "follow-bottom",
    );
  });
});
