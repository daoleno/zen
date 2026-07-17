import { describe, expect, test } from "bun:test";
import {
  beginComposerStop,
  reconcileComposerStopLatch,
  releaseComposerStopLatch,
} from "./composerStopLatch";

describe("Composer per-Activity Stop latch", () => {
  test("accepts Stop exactly once for the same current Activity", () => {
    const first = beginComposerStop(undefined, "activity-a");
    expect(first).toEqual({ accepted: true, latchedActivityId: "activity-a" });
    expect(beginComposerStop(first.latchedActivityId, "activity-a")).toEqual({
      accepted: false,
      latchedActivityId: "activity-a",
    });
    expect(reconcileComposerStopLatch(first.latchedActivityId, "activity-a"))
      .toBe("activity-a");
  });

  test("settlement or successor promotion unlocks the next Activity", () => {
    expect(reconcileComposerStopLatch("activity-a", undefined)).toBeUndefined();
    const reconciled = reconcileComposerStopLatch("activity-a", "activity-b");
    expect(reconciled).toBeUndefined();
    expect(beginComposerStop(reconciled, "activity-b")).toEqual({
      accepted: true,
      latchedActivityId: "activity-b",
    });
  });

  test("a synchronous transport failure can release the same Activity", () => {
    const first = beginComposerStop(undefined, "activity-a");
    const released = releaseComposerStopLatch(first.latchedActivityId, "activity-a");
    expect(released).toBeUndefined();
    expect(beginComposerStop(released, "activity-a")).toEqual(first);
  });

  test("a delayed failure for the old Activity cannot unlock its successor", () => {
    expect(releaseComposerStopLatch("activity-b", "activity-a"))
      .toBe("activity-b");
  });
});
