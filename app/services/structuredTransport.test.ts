// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  dispatchStructuredCommand,
  normalizeStructuredInputAccepted,
  sendWebSocketMessageNow,
  structuredActionMessage,
  structuredInputMessage,
} from "./structuredWebSocketTransport";

class FakeEventSource {
  handlers = new Map<string, Array<(payload: any) => void>>();

  on(type: string, handler: (payload: any) => void) {
    this.handlers.set(type, [...(this.handlers.get(type) ?? []), handler]);
  }

  off(type: string, handler: (payload: any) => void) {
    this.handlers.set(
      type,
      (this.handlers.get(type) ?? []).filter((candidate) => candidate !== handler),
    );
  }

  emit(type: string, payload: any) {
    for (const handler of this.handlers.get(type) ?? []) {
      handler(payload);
    }
  }

  listenerCount() {
    return Array.from(this.handlers.values()).reduce(
      (count, handlers) => count + handlers.length,
      0,
    );
  }
}

describe("structured provider transport", () => {
  test("sends immediately on an open socket", () => {
    const sent: string[] = [];
    sendWebSocketMessageNow(
      { readyState: 1, send: (value: string) => sent.push(value) },
      { type: "send_input", request_id: "request-a" },
    );
    expect(sent).toEqual([
      JSON.stringify({ type: "send_input", request_id: "request-a" }),
    ]);
  });

  test("rejects a closing socket without retaining the command for reconnect", () => {
    const sent: string[] = [];
    expect(() =>
      sendWebSocketMessageNow(
        { readyState: 2, send: (value: string) => sent.push(value) },
        { type: "send_input", request_id: "request-b" },
      ),
    ).toThrow("Daemon is not connected.");
    expect(sent).toEqual([]);
  });

  test("busy thread controls carry the real queue and conversation identity", () => {
    expect(structuredInputMessage({
      requestId: "request-new",
      agentId: "host-b",
      text: "/new\n",
      conversationScopeKey: "brain-thread:current",
      turnId: "turn-control",
      turnStartedAt: "2026-07-15T10:00:00.000Z",
      turnQueued: true,
      turnConversationIdentity: "session:old",
    })).toMatchObject({
      type: "send_input",
      request_id: "request-new",
      conversation_scope_key: "brain-thread:current",
      turn_id: "turn-control",
      turn_queued: true,
      turn_conversation_identity: "session:old",
    });
  });

  test("queue acknowledgement retains daemon epoch and causal revision", () => {
    expect(normalizeStructuredInputAccepted({
      turn_id: "turn-control",
      queued: true,
      turn_epoch: "daemon-a",
      turn_revision: 7,
    }, "fallback")).toEqual({
      turnId: "turn-control",
      queued: true,
      turnEpoch: "daemon-a",
      turnRevision: 7,
    });
  });

  test("Stop carries a correlated request and the exact public turn", () => {
    expect(structuredActionMessage({
      requestId: "request-stop",
      agentId: "host-a",
      action: "pause",
      conversationScopeKey: "brain-thread:current",
      turnId: "turn-a",
      turnStartedAt: "2026-07-15T10:00:00.000Z",
    })).toEqual({
      type: "send_action",
      request_id: "request-stop",
      agent_id: "host-a",
      action: "pause",
      conversation_scope_key: "brain-thread:current",
      turn_id: "turn-a",
      turn_started_at: "2026-07-15T10:00:00.000Z",
    });
  });

  test("successful send returns immediately and a delayed ACK refines it", async () => {
    const events = new FakeEventSource();
    let sent = false;
    const receipt = dispatchStructuredCommand({
      requestId: "request-delayed",
      eventSource: events,
      confirmedType: "input_accepted",
      rejectedType: "input_rejected",
      matches: (payload) => payload.request_id === "request-delayed",
      normalizeConfirmed: (payload) => payload.value,
      sendNow: () => {
        sent = true;
      },
      timeoutMs: 100,
    });
    let settled = false;
    void receipt.outcome.then(() => {
      settled = true;
    });
    await Promise.resolve();
    expect(sent).toBe(true);
    expect(settled).toBe(false);

    events.emit("input_accepted", {
      request_id: "request-other",
      value: "wrong",
    });
    expect(settled).toBe(false);
    events.emit("input_accepted", {
      request_id: "request-delayed",
      value: "accepted",
    });
    expect(await receipt.outcome).toEqual({
      kind: "confirmed",
      value: "accepted",
    });
    expect(events.listenerCount()).toBe(0);
  });

  test("missing ACK and ambiguous daemon error are unconfirmed, never rejected", async () => {
    const missingEvents = new FakeEventSource();
    const missing = dispatchStructuredCommand({
      requestId: "request-missing",
      eventSource: missingEvents,
      confirmedType: "input_accepted",
      rejectedType: "input_rejected",
      matches: (payload) => payload.request_id === "request-missing",
      normalizeConfirmed: (payload) => payload,
      sendNow: () => {},
      timeoutMs: 1,
    });
    expect(await missing.outcome).toEqual({ kind: "unconfirmed" });

    const ambiguousEvents = new FakeEventSource();
    const ambiguous = dispatchStructuredCommand({
      requestId: "request-ambiguous",
      eventSource: ambiguousEvents,
      confirmedType: "input_accepted",
      rejectedType: "input_rejected",
      matches: (payload) => payload.request_id === "request-ambiguous",
      normalizeConfirmed: (payload) => payload,
      sendNow: () => {},
      timeoutMs: 100,
    });
    ambiguousEvents.emit("input_unconfirmed", {
      request_id: "request-ambiguous",
    });
    expect(await ambiguous.outcome).toEqual({ kind: "unconfirmed" });
  });

  test("only the correlated operation rejection produces a retryable failure", async () => {
    const events = new FakeEventSource();
    const receipt = dispatchStructuredCommand({
      requestId: "request-rejected",
      eventSource: events,
      confirmedType: "input_accepted",
      rejectedType: "input_rejected",
      matches: (payload) => payload.request_id === "request-rejected",
      normalizeConfirmed: (payload) => payload,
      sendNow: () => {},
      timeoutMs: 100,
    });
    events.emit("input_rejected", {
      request_id: "request-other",
      code: "wrong",
      message: "wrong request",
    });
    events.emit("input_rejected", {
      request_id: "request-rejected",
      code: "structured_lifecycle_syncing",
      message: "Refresh and retry.",
    });
    expect(await receipt.outcome).toEqual({
      kind: "rejected",
      rejection: {
        requestId: "request-rejected",
        code: "structured_lifecycle_syncing",
        message: "Refresh and retry.",
      },
    });
  });

  test("sendNow failure throws synchronously and removes acknowledgement listeners", () => {
    const events = new FakeEventSource();
    expect(() => dispatchStructuredCommand({
      requestId: "request-local-failure",
      eventSource: events,
      confirmedType: "input_accepted",
      rejectedType: "input_rejected",
      matches: (payload) => payload.request_id === "request-local-failure",
      normalizeConfirmed: (payload) => payload,
      sendNow: () => {
        throw new Error("socket closed");
      },
    })).toThrow("socket closed");
    expect(events.listenerCount()).toBe(0);
  });
});
