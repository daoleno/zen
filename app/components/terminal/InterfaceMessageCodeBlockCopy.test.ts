// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  CODE_BLOCK_COPIED_RESET_MS,
  CODE_BLOCK_COPY_TOUCH_SLOP_PX,
  codeBlockCopyMovedBeyondSlop,
  codeBlockCopyShouldCommit,
  createCodeBlockCopyFeedback,
} from "./InterfaceMessageCodeBlockCopy";

describe("fenced Markdown code copy feedback", () => {
  test("one copy request starts one immediate write with the exact payload", async () => {
    const copied: string[] = [];
    let finishWrite: (() => void) | undefined;
    const feedback = createCodeBlockCopyFeedback({
      copyText: (text) => {
        copied.push(text);
        return new Promise<void>((resolve) => {
          finishWrite = resolve;
        });
      },
      onCopiedChange: () => {},
      scheduleReset: () => Symbol("timer"),
      cancelReset: () => {},
    });
    const payload = "const fence = \"```\";\r\n  keep indentation  \n";

    const request = feedback.copy(payload);
    expect(copied).toEqual([payload]);
    finishWrite?.();
    await request;
  });

  test("transitions to success as soon as the write succeeds", async () => {
    const states: boolean[] = [];
    let finishWrite: (() => void) | undefined;
    let reset: (() => void) | undefined;
    let resetDelay: number | undefined;
    const feedback = createCodeBlockCopyFeedback({
      copyText: () =>
        new Promise<void>((resolve) => {
          finishWrite = resolve;
        }),
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: (callback, delayMs) => {
        reset = callback;
        resetDelay = delayMs;
        return Symbol("timer");
      },
      cancelReset: () => {},
    });

    const request = feedback.copy("payload");
    expect(states).toEqual([]);

    finishWrite?.();
    await request;
    expect(states).toEqual([true]);
    expect(resetDelay).toBe(CODE_BLOCK_COPIED_RESET_MS);

    reset?.();
    expect(states).toEqual([true, false]);
  });

  test("repeated successes replace the reset without stale timer races", async () => {
    const states: boolean[] = [];
    const resets: Array<() => void> = [];
    const canceled: number[] = [];
    const feedback = createCodeBlockCopyFeedback({
      copyText: async () => {},
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: (callback) => {
        resets.push(callback);
        return resets.length - 1;
      },
      cancelReset: (timer) => {
        canceled.push(timer);
      },
    });

    await feedback.copy("first");
    await feedback.copy("second");

    expect(states).toEqual([true, true]);
    expect(canceled).toEqual([0]);

    resets[0]?.();
    expect(states).toEqual([true, true]);
    feedback.dispose();
    expect(canceled).toEqual([0, 1]);
    resets[1]?.();
    expect(states).toEqual([true, true]);
  });

  test("a failed write is quiet and a later request recovers", async () => {
    const states: boolean[] = [];
    let attempts = 0;
    const feedback = createCodeBlockCopyFeedback({
      copyText: async () => {
        attempts += 1;
        if (attempts === 1) {
          throw new Error("clipboard unavailable");
        }
      },
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: () => Symbol("timer"),
      cancelReset: () => {},
    });

    await expect(feedback.copy("first")).resolves.toBeUndefined();
    expect(states).toEqual([]);

    await feedback.copy("second");
    expect(attempts).toBe(2);
    expect(states).toEqual([true]);
  });

  test("dispose cancels reset and blocks late async feedback", async () => {
    const states: boolean[] = [];
    const canceled: symbol[] = [];
    let finishLateWrite: (() => void) | undefined;
    const timer = Symbol("timer");
    const feedback = createCodeBlockCopyFeedback({
      copyText: (text) =>
        text === "late"
          ? new Promise<void>((resolve) => {
              finishLateWrite = resolve;
            })
          : Promise.resolve(),
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: () => timer,
      cancelReset: (value) => {
        canceled.push(value);
      },
    });

    await feedback.copy("first");
    const lateRequest = feedback.copy("late");
    feedback.dispose();
    finishLateWrite?.();
    await lateRequest;

    expect(canceled).toEqual([timer]);
    expect(states).toEqual([true]);
  });
});

describe("fenced Markdown code copy touch ownership", () => {
  test("retains a clean tap and yields only after real movement", () => {
    expect(CODE_BLOCK_COPY_TOUCH_SLOP_PX).toBe(10);
    expect(codeBlockCopyMovedBeyondSlop(4, 8, 14, 18)).toBe(false);
    expect(codeBlockCopyMovedBeyondSlop(4, 8, 15, 8)).toBe(true);
    expect(
      codeBlockCopyShouldCommit({
        gestureActive: true,
        userMovedBeyondSlop: false,
      }),
    ).toBe(true);
    expect(
      codeBlockCopyShouldCommit({
        gestureActive: true,
        userMovedBeyondSlop: true,
      }),
    ).toBe(false);
  });

  test("wires one shared Android/iOS responder release to one copy request", () => {
    const source = readFileSync(
      join(import.meta.dir, "InterfaceMessageCodeBlock.tsx"),
      "utf8",
    );
    const releaseHandler = source.slice(
      source.indexOf("const handleCopyResponderRelease"),
      source.indexOf("useEffect(() =>"),
    );

    expect(releaseHandler.match(/copyCode\(\)/g)).toHaveLength(1);
    expect(source).toContain(
      "onResponderTerminationRequest={() => copyTouchRef.current.moved}",
    );
    expect(source).toContain(
      "onResponderRelease={handleCopyResponderRelease}",
    );
    expect(source).toContain("copyFeedback.copy(text)");
    expect(source).not.toContain("copyFeedback.copy(prepared.text)");
    expect(source).not.toContain("AnimatedPressable");
  });
});
