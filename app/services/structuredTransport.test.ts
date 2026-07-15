// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  normalizeStructuredInputAccepted,
  sendWebSocketMessageNow,
  structuredActionMessage,
  structuredInputMessage,
} from "./structuredWebSocketTransport";

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
});
