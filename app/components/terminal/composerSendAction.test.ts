import { describe, expect, test } from "bun:test";
import { resolveComposerSendAction } from "./composerSendAction";

describe("composer send action", () => {
  test("running Activity with a draft morphs the single slot to Send", () => {
    const result = resolveComposerSendAction({
      canSend: true,
      connected: true,
      hasComposerContent: true,
      interrupting: false,
      activityRunning: true,
    });
    expect(result.showStopButton).toBe(false);
    expect(result.stopEnabled).toBe(true);
    expect(result.sendEnabled).toBe(true);
    expect(result.sendLabel).toBe("Send message");
  });

  test("running Activity without a draft exposes Stop", () => {
    const result = resolveComposerSendAction({
      canSend: false,
      connected: true,
      hasComposerContent: false,
      interrupting: false,
      activityRunning: true,
    });
    expect(result.showStopButton).toBe(true);
    expect(result.stopEnabled).toBe(true);
    expect(result.stopLabel).toBe("Stop response");
    expect(result.sendEnabled).toBe(false);
    expect(result.sendLabel).toBe("Send message");
  });

  test("reconnect keeps the draft-facing Send slot visible but disabled", () => {
    const result = resolveComposerSendAction({
      canSend: false,
      connected: false,
      hasComposerContent: true,
      interrupting: false,
      activityRunning: true,
    });
    expect(result.showStopButton).toBe(false);
    expect(result.stopEnabled).toBe(false);
    expect(result.sendEnabled).toBe(false);
  });

  test("draft content retains Send while the current Activity is interrupting", () => {
    const result = resolveComposerSendAction({
      canSend: true,
      connected: true,
      hasComposerContent: true,
      interrupting: true,
      activityRunning: true,
    });
    expect(result.showStopButton).toBe(false);
    expect(result.stopEnabled).toBe(false);
    expect(result.stopLabel).toBe("Stopping response");
    expect(result.sendEnabled).toBe(true);
    expect(result.sendLabel).toBe("Send message");
  });

  test("a cleared composer during Activity exposes Stop", () => {
    const result = resolveComposerSendAction({
      canSend: false,
      connected: true,
      hasComposerContent: false,
      interrupting: false,
      activityRunning: true,
    });
    expect(result.showStopButton).toBe(true);
    expect(result.stopEnabled).toBe(true);
    expect(result.sendLabel).toBe("Send message");
    expect(result.sendEnabled).toBe(false);
  });

  test("successful send during Activity returns the same slot from Send to Stop", () => {
    const common = {
      canSend: true,
      connected: true,
      elapsedStartedAt: "2026-07-15T10:00:00.000Z",
      interrupting: false,
      activityRunning: true,
    };
    const whileEditing = resolveComposerSendAction({
      ...common,
      hasComposerContent: true,
    });
    const afterLiveSend = resolveComposerSendAction({
      ...common,
      canSend: false,
      hasComposerContent: false,
    });
    expect(whileEditing).toMatchObject({
      showStopButton: false,
      providerActivityStartedAt: common.elapsedStartedAt,
    });
    expect(afterLiveSend).toMatchObject({
      showStopButton: true,
      providerActivityStartedAt: common.elapsedStartedAt,
    });
  });
});
