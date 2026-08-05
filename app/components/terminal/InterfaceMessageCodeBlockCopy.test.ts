import { describe, expect, test } from "bun:test";
import {
  CODE_BLOCK_COPIED_RESET_MS,
  createCodeBlockCopyFeedback,
} from "./InterfaceMessageCodeBlockCopy";

describe("fenced Markdown code copy feedback", () => {
  test("copies the exact code payload without display metadata", async () => {
    const copied: string[] = [];
    const feedback = createCodeBlockCopyFeedback({
      copyText: async (text) => {
        copied.push(text);
      },
      onCopiedChange: () => {},
      scheduleReset: () => Symbol("timer"),
      cancelReset: () => {},
    });
    const payload = "const fence = \"```\";\r\n  keep indentation  \n";

    await feedback.copy(payload);

    expect(copied).toEqual([payload]);
  });

  test("shows copied state briefly and resets it safely", async () => {
    const states: boolean[] = [];
    const scheduled: {
      reset?: () => void;
      delay?: number;
    } = {};
    let canceled = false;
    const feedback = createCodeBlockCopyFeedback({
      copyText: async () => {},
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: (callback, delayMs) => {
        scheduled.reset = callback;
        scheduled.delay = delayMs;
        return Symbol("timer");
      },
      cancelReset: () => {
        canceled = true;
      },
    });

    await feedback.copy("payload");
    expect(states).toEqual([true]);
    expect(scheduled.delay).toBe(CODE_BLOCK_COPIED_RESET_MS);

    scheduled.reset?.();
    expect(states).toEqual([true, false]);

    await feedback.copy("again");
    feedback.dispose();
    scheduled.reset?.();
    expect(canceled).toBe(true);
    expect(states).toEqual([true, false, true]);
  });
});
