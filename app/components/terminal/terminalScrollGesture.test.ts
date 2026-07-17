import { describe, expect, test } from "bun:test";
import {
  TERMINAL_SCROLL_GESTURE_CONTROLLER_SOURCE,
  TERMINAL_SCROLL_BATCH_INTERVAL_MS,
  TERMINAL_SCROLL_CANCEL_REASONS,
  TERMINAL_SCROLL_INERTIA_MAX_FRAMES,
  TERMINAL_SCROLL_MAX_BATCH_LINES,
  type TerminalScrollGestureController,
} from "./terminalScrollGesture";

function createTerminalScrollGestureController(): TerminalScrollGestureController {
  return new Function(
    `return ${TERMINAL_SCROLL_GESTURE_CONTROLLER_SOURCE};`,
  )() as TerminalScrollGestureController;
}

function claimFastUpwardGesture() {
  const gesture = createTerminalScrollGestureController();
  gesture.start(20, 100, 0, false);
  expect(gesture.move(20, 60, 32, 10)).toBe(true);
  expect(gesture.frame(32).lines).toBe(4);
  expect(gesture.end(32)).toBe(true);
  return gesture;
}

describe("Terminal native scroll gesture", () => {
  test("a tap, horizontal motion, and active selection never become scroll", () => {
    const tap = createTerminalScrollGestureController();
    tap.start(20, 20, 0, false);
    expect(tap.move(20, 17, 8, 10)).toBe(false);
    expect(tap.end(8)).toBe(false);
    expect(tap.frame(16)).toEqual({ lines: 0, keepAnimating: false });

    const horizontal = createTerminalScrollGestureController();
    horizontal.start(20, 20, 0, false);
    expect(horizontal.move(60, 25, 16, 10)).toBe(false);
    expect(horizontal.end(16)).toBe(false);

    const diagonal = createTerminalScrollGestureController();
    diagonal.start(20, 20, 0, false);
    expect(diagonal.move(28, 13, 16, 10)).toBe(false);
    expect(diagonal.end(16)).toBe(false);

    const selection = createTerminalScrollGestureController();
    selection.start(20, 20, 0, true);
    expect(selection.move(20, 80, 16, 10)).toBe(false);
    expect(selection.end(16)).toBe(false);
  });

  test("emits incremental lines on animation frames and retains slow-drag fractions", () => {
    const gesture = createTerminalScrollGestureController();
    gesture.start(10, 100, 0, false);

    expect(gesture.move(10, 93, 8, 10)).toBe(true);
    expect(gesture.frame(8)).toEqual({ lines: 0, keepAnimating: true });
    expect(gesture.frame(16)).toEqual({ lines: 0, keepAnimating: true });

    expect(gesture.move(10, 87, 20, 10)).toBe(true);
    expect(gesture.frame(32)).toEqual({ lines: 1, keepAnimating: true });

    expect(gesture.move(10, 84, 36, 10)).toBe(true);
    expect(gesture.move(10, 79, 44, 10)).toBe(true);
    expect(gesture.frame(48)).toEqual({ lines: 1, keepAnimating: true });
  });

  test("direction reversal consumes the existing fraction without a jump", () => {
    const gesture = createTerminalScrollGestureController();
    gesture.start(10, 100, 0, false);

    expect(gesture.move(10, 91, 16, 10)).toBe(true);
    expect(gesture.frame(16).lines).toBe(0);

    expect(gesture.move(10, 97, 32, 10)).toBe(true);
    expect(gesture.frame(32).lines).toBe(0);
    expect(gesture.move(10, 108, 48, 10)).toBe(true);
    expect(gesture.frame(48).lines).toBe(0);
    expect(gesture.move(10, 111, 64, 10)).toBe(true);
    expect(gesture.frame(64).lines).toBe(-1);
  });

  test("coalesces to one bounded batch per 16ms frame without an 80-120ms gate", () => {
    expect(TERMINAL_SCROLL_BATCH_INTERVAL_MS).toBeLessThanOrEqual(32);
    expect(TERMINAL_SCROLL_BATCH_INTERVAL_MS).toBe(16);

    const gesture = createTerminalScrollGestureController();
    gesture.start(0, 400, 0, false);
    expect(gesture.move(0, 200, 4, 10)).toBe(true);
    expect(gesture.move(0, 0, 8, 10)).toBe(true);

    expect(gesture.frame(8).lines).toBe(0);
    const first = gesture.frame(16);
    expect(first.lines).toBe(TERMINAL_SCROLL_MAX_BATCH_LINES);
    expect(first.keepAnimating).toBe(true);
    expect(gesture.frame(20).lines).toBe(0);
    expect(gesture.frame(32).lines).toBe(TERMINAL_SCROLL_MAX_BATCH_LINES);
  });

  test("recent velocity produces deterministic decaying inertia with a hard stop", () => {
    const gesture = claimFastUpwardGesture();
    const lines: number[] = [];
    let keepAnimating = true;
    let frames = 0;

    while (keepAnimating && frames <= TERMINAL_SCROLL_INERTIA_MAX_FRAMES + 2) {
      frames += 1;
      const frame = gesture.frame(32 + frames * TERMINAL_SCROLL_BATCH_INTERVAL_MS);
      keepAnimating = frame.keepAnimating;
      if (frame.lines !== 0) {
        lines.push(frame.lines);
      }
    }

    expect(lines.length).toBeGreaterThan(0);
    expect(lines.every((line) => line > 0 && line <= TERMINAL_SCROLL_MAX_BATCH_LINES)).toBe(true);
    expect(frames).toBeLessThanOrEqual(TERMINAL_SCROLL_INERTIA_MAX_FRAMES);
    expect(keepAnimating).toBe(false);
    expect(gesture.frame(10_000)).toEqual({ lines: 0, keepAnimating: false });
  });

  test("every cancellation reason stops pending fractions and inertia immediately", () => {
    expect(TERMINAL_SCROLL_CANCEL_REASONS).toEqual([
      "new-touch",
      "input",
      "selection",
      "route-blur",
      "disconnect",
      "session-change",
      "jump-live",
      "touch-cancel",
    ]);

    for (const reason of TERMINAL_SCROLL_CANCEL_REASONS) {
      const gesture = claimFastUpwardGesture();
      gesture.cancel(reason);
      expect(gesture.frame(48), reason).toEqual({ lines: 0, keepAnimating: false });
    }
  });

  test("a new touch cancels prior inertia before tracking the next gesture", () => {
    const gesture = claimFastUpwardGesture();
    gesture.start(50, 50, 40, false);
    expect(gesture.frame(48)).toEqual({ lines: 0, keepAnimating: true });
    expect(gesture.end(48)).toBe(false);
    expect(gesture.frame(64)).toEqual({ lines: 0, keepAnimating: false });
  });

  test("the WebView receives the exact tested controller implementation", () => {
    const gesture = createTerminalScrollGestureController();

    gesture.start(10, 50, 0, false);
    expect(gesture.move(10, 90, 16, 10)).toBe(true);
    expect(gesture.frame(16).lines).toBe(-4);
  });
});
