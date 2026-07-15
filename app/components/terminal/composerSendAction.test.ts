// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { resolveComposerSendAction } from "./composerSendAction";

describe("composer send action", () => {
  test("running turn with a draft morphs the single slot to Send", () => {
    const result = resolveComposerSendAction({
      canSend: true,
      connected: true,
      hasComposerContent: true,
      interrupting: false,
      requestRunning: true,
      sending: false,
      startingNewChat: false,
    });
    expect(result.primaryAction).toBe("send");
    expect(result.showStopButton).toBe(false);
    expect(result.stopEnabled).toBe(true);
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
    expect(result.primaryAction).toBe("stop");
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
      requestRunning: true,
      sending: false,
      startingNewChat: false,
    });
    expect(result.primaryAction).toBe("send");
    expect(result.showStopButton).toBe(false);
    expect(result.showStopIndicator).toBe(false);
    expect(result.stopEnabled).toBe(false);
    expect(result.sendEnabled).toBe(false);
  });

  test("draft content retains Send even while the current turn is interrupting", () => {
    const result = resolveComposerSendAction({
      canSend: true,
      connected: true,
      hasComposerContent: true,
      interrupting: true,
      requestRunning: true,
      sending: false,
      startingNewChat: false,
    });
    expect(result.primaryAction).toBe("send");
    expect(result.stopEnabled).toBe(false);
    expect(result.stopLabel).toBe("Stopping response");
    expect(result.sendEnabled).toBe(true);
    expect(result.sendLabel).toBe("Send message");
  });

  test("an in-flight queue submission returns the cleared slot to Stop", () => {
    const result = resolveComposerSendAction({
      canSend: false,
      connected: true,
      hasComposerContent: false,
      interrupting: false,
      requestRunning: true,
      sending: true,
      startingNewChat: false,
    });
    expect(result.primaryAction).toBe("stop");
    expect(result.showStopButton).toBe(true);
    expect(result.stopEnabled).toBe(true);
    expect(result.sendLabel).toBe("Send message");
    expect(result.sendEnabled).toBe(false);
  });

  test("successful active-turn submission returns the same slot from Send to Stop", () => {
    const common = {
      canSend: true,
      connected: true,
      elapsedStartedAt: "2026-07-15T10:00:00.000Z",
      interrupting: false,
      requestRunning: true,
      sending: false,
      startingNewChat: false,
    };
    const whileEditing = resolveComposerSendAction({
      ...common,
      hasComposerContent: true,
    });
    const afterAcceptedSend = resolveComposerSendAction({
      ...common,
      canSend: false,
      hasComposerContent: false,
    });
    expect(whileEditing).toMatchObject({
      primaryAction: "send",
      workingTurnStartedAt: common.elapsedStartedAt,
    });
    expect(afterAcceptedSend).toMatchObject({
      primaryAction: "stop",
      workingTurnStartedAt: common.elapsedStartedAt,
    });
  });
});
