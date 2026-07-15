// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

describe("structured input submission contract", () => {
  const transportSource = readFileSync(
    join(import.meta.dir, "useCodexMessageTransport.ts"),
    "utf8",
  );

  test("successful socket dispatch releases Composer before ACK refinement", () => {
    const sendNow = transportSource.indexOf("receipt = wsClient.sendInput(");
    const optimistic = transportSource.indexOf(
      "const pendingMessageId = addPendingUserMessage",
      sendNow,
    );
    const unlock = transportSource.indexOf("unlockSend();", optimistic);
    const observe = transportSource.indexOf(
      "observeInputOutcome(pendingMessageId",
      unlock,
    );
    expect(sendNow).toBeGreaterThan(-1);
    expect(optimistic).toBeGreaterThan(sendNow);
    expect(unlock).toBeGreaterThan(optimistic);
    expect(observe).toBeGreaterThan(unlock);
    expect(transportSource).not.toContain("await wsClient.sendInput");
  });

  test("operational send and Stop failures are nonmodal", () => {
    expect(transportSource).not.toContain("Alert.alert");
    expect(transportSource).not.toContain("from \"react-native\";\nimport { Alert");
    expect(transportSource).toContain("setOperationalError(");
  });

  test("async outcomes never remove the optimistic row or restore its draft", () => {
    const observerStart = transportSource.indexOf(
      "const observeInputOutcome",
    );
    const observerEnd = transportSource.indexOf(
      "const submitTextToCodex",
      observerStart,
    );
    const observer = transportSource.slice(observerStart, observerEnd);
    expect(observer).not.toContain("removePendingUserMessage");
    expect(observer).not.toContain("restoreDraft");
    expect(observer).toContain("rejectPendingUserMessage");
  });

  test("inline retry reuses the durable execution identity", () => {
    const retryStart = transportSource.indexOf(
      "const retryPendingUserMessage",
    );
    const retryEnd = transportSource.indexOf(
      "const startNewCodexChat",
      retryStart,
    );
    const retry = transportSource.slice(retryStart, retryEnd);
    expect(retry).toContain("turnId: message.turnId");
    expect(retry).toContain("turnStartedAt: message.turnStartedAt");
    expect(retry).toContain("`${message.sentText}\\n`");
    expect(retry).not.toContain("createStructuredTurnIdentity");
    expect(retry).not.toContain("addPendingUserMessage");
  });
});
