// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { resolveComposerSendAction } from "./composerSendAction";

describe("composer send action", () => {
  test("running turn with a draft keeps Send enabled", () => {
    const result = resolveComposerSendAction({
      canSend: true,
      connected: true,
      hasComposerContent: true,
      interrupting: false,
      requestRunning: true,
      sending: false,
      startingNewChat: false,
    });
    expect(result.showStopButton).toBe(false);
    expect(result.sendEnabled).toBe(true);
    expect(result.sendLabel).toBe("Send message");
  });

  test("running turn without a draft exposes Stop", () => {
    const result = resolveComposerSendAction({
      canSend: false,
      connected: true,
      hasComposerContent: false,
      interrupting: false,
      requestRunning: true,
      sending: false,
      startingNewChat: false,
    });
    expect(result.showStopButton).toBe(true);
    expect(result.sendEnabled).toBe(true);
    expect(result.sendLabel).toBe("Stop response");
  });
});
