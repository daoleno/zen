import { describe, expect, test } from "bun:test";
import {
  beginComposerStop,
  reconcileComposerStopLatch,
  releaseComposerStopLatch,
} from "./composerStopLatch";

describe("Composer per-turn Stop latch", () => {
  test("accepts Stop exactly once for the same authoritative turn", () => {
    const first = beginComposerStop(undefined, "turn-a");
    expect(first).toEqual({ accepted: true, latchedTurnId: "turn-a" });
    expect(beginComposerStop(first.latchedTurnId, "turn-a")).toEqual({
      accepted: false,
      latchedTurnId: "turn-a",
    });
    expect(reconcileComposerStopLatch(first.latchedTurnId, "turn-a"))
      .toBe("turn-a");
  });

  test("settlement or successor promotion unlocks the next turn", () => {
    expect(reconcileComposerStopLatch("turn-a", undefined)).toBeUndefined();
    const reconciled = reconcileComposerStopLatch("turn-a", "turn-b");
    expect(reconciled).toBeUndefined();
    expect(beginComposerStop(reconciled, "turn-b")).toEqual({
      accepted: true,
      latchedTurnId: "turn-b",
    });
  });

  test("a synchronous transport failure can release the same turn", () => {
    const first = beginComposerStop(undefined, "turn-a");
    const released = releaseComposerStopLatch(first.latchedTurnId, "turn-a");
    expect(released).toBeUndefined();
    expect(beginComposerStop(released, "turn-a")).toEqual(first);
  });

  test("a delayed failure for the old turn cannot unlock its successor", () => {
    expect(releaseComposerStopLatch("turn-b", "turn-a")).toBe("turn-b");
  });
});
