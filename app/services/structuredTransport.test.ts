import { describe, expect, test } from "bun:test";
import {
  dispatchStructuredCommand,
  sendWebSocketMessageNow,
  structuredActionMessage,
  structuredInputMessage,
} from "./structuredWebSocketTransport";

type Handler = (payload: any) => void;

class FakeEventSource {
  handlers = new Map<string, Handler[]>();

  on(type: string, handler: Handler) {
    this.handlers.set(type, [...(this.handlers.get(type) ?? []), handler]);
  }

  off(type: string, handler: Handler) {
    const remaining = (this.handlers.get(type) ?? []).filter(
      (candidate) => candidate !== handler,
    );
    if (remaining.length === 0) {
      this.handlers.delete(type);
    } else {
      this.handlers.set(type, remaining);
    }
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

function command(events: FakeEventSource, requestId: string, sendNow = () => {}) {
  return dispatchStructuredCommand({
    requestId,
    eventSource: events,
    sentType: "input_sent",
    failedType: "input_failed",
    matches: (payload) =>
      payload.serverId === "server-a" && payload.request_id === requestId,
    matchesConnection: (payload) => payload.serverId === "server-a",
    sendNow,
  });
}

describe("structured provider transport", () => {
  test("sends exactly once on an open socket", () => {
    const sent: string[] = [];
    sendWebSocketMessageNow(
      { readyState: 1, send: (value: string) => sent.push(value) },
      { type: "send_input", request_id: "request-a" },
    );
    expect(sent).toEqual([
      JSON.stringify({ type: "send_input", request_id: "request-a" }),
    ]);
  });

  test("rejects a closing socket without retaining a command", () => {
    const sent: string[] = [];
    expect(() =>
      sendWebSocketMessageNow(
        { readyState: 2, send: (value: string) => sent.push(value) },
        { type: "send_input", request_id: "request-b" },
      ),
    ).toThrow("Daemon is not connected.");
    expect(sent).toEqual([]);
  });

  test("input and Stop carry only immediate provider-call facts", () => {
    expect(structuredInputMessage({
      requestId: "request-input",
      agentId: "agent-a",
      text: "hello\n",
    })).toEqual({
      type: "send_input",
      request_id: "request-input",
      agent_id: "agent-a",
      text: "hello\n",
    });
    expect(structuredActionMessage({
      requestId: "request-stop",
      agentId: "agent-a",
      action: "pause",
    })).toEqual({
      type: "send_action",
      request_id: "request-stop",
      agent_id: "agent-a",
      action: "pause",
    });
  });

  test("a success ACK reports only that the immediate send returned", async () => {
    const events = new FakeEventSource();
    let sends = 0;
    const receipt = command(events, "request-sent", () => {
      sends += 1;
    });
    events.emit("input_sent", {
      serverId: "server-a",
      request_id: "other-request",
    });
    expect(events.listenerCount()).toBe(3);
    events.emit("input_sent", {
      serverId: "server-a",
      request_id: "request-sent",
    });
    expect(await receipt.outcome).toEqual({ kind: "sent" });
    expect(sends).toBe(1);
    expect(events.listenerCount()).toBe(0);
  });

  test("only a correlated explicit failure fails the current attempt", async () => {
    const events = new FakeEventSource();
    const receipt = command(events, "request-current");
    events.emit("input_failed", {
      serverId: "server-a",
      request_id: "request-previous",
      code: "old_failure",
      message: "old attempt",
    });
    expect(events.listenerCount()).toBe(3);
    events.emit("input_failed", {
      serverId: "server-a",
      request_id: "request-current",
      code: "send_input_failed",
      message: "provider refused input",
    });
    expect(await receipt.outcome).toEqual({
      kind: "failed",
      failure: {
        requestId: "request-current",
        code: "send_input_failed",
        message: "provider refused input",
      },
    });
    expect(events.listenerCount()).toBe(0);
  });

  test("connection close cleans observers without inventing a disposition", async () => {
    const events = new FakeEventSource();
    let sends = 0;
    const receipt = command(events, "request-uncertain", () => {
      sends += 1;
    });
    events.emit("disconnected", {
      serverId: "server-other",
      reason: "transport_closed",
    });
    expect(events.listenerCount()).toBe(3);
    events.emit("disconnected", {
      serverId: "server-a",
      reason: "transport_closed",
    });
    expect(await receipt.outcome).toEqual({ kind: "connection_closed" });
    events.emit("input_sent", {
      serverId: "server-a",
      request_id: "request-uncertain",
    });
    expect(sends).toBe(1);
    expect(events.listenerCount()).toBe(0);
  });

  test("synchronous send failure throws and removes all observers", () => {
    const events = new FakeEventSource();
    expect(() => command(events, "request-local-failure", () => {
      throw new Error("socket closed");
    })).toThrow("socket closed");
    expect(events.listenerCount()).toBe(0);
  });
});
