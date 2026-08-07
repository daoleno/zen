import { describe, expect, test } from "bun:test";

import { normalizeCodexConversation } from "./codexConversation";
import { projectZenTimeline } from "../components/terminal/projectZenTimeline";
import { reconcileConversationDeltaEvents } from "../components/terminal/interfaceConversationReconciliation";

const T = (sec: number) =>
  new Date(Date.parse("2026-08-07T00:00:10Z") + sec * 1000).toISOString();

/**
 * Daemon wire shape for OpenCode/Pi adapters: tool lifecycle is projected with
 * kind "tool_call", intentional reasoning with kind "reasoning", stable stored
 * part IDs as event ids, state.input as raw JSON, and status
 * running/pending/completed/failed. The app's canonical
 * CodexConversationEventKind does not declare these provider-adaptive kinds —
 * the wire boundary owns that mapping.
 */
interface OpenCodeDaemonEvent {
  id: string;
  seq: number;
  kind: string;
  role?: "user" | "assistant";
  body?: string;
  tool_name?: string;
  call_id?: string;
  input?: string;
  output?: string;
  status?: string;
  partial?: boolean;
  transient?: boolean;
  timestamp?: string;
}

function openCodeDaemonEvents(settled: boolean): OpenCodeDaemonEvent[] {
  return [
    {
      id: "prt_user",
      seq: 1,
      kind: "user_message",
      role: "user",
      body: "run both tools",
      timestamp: T(1),
    },
    {
      id: "prt_reason",
      seq: 2,
      kind: "reasoning",
      body: "thinking hard",
      timestamp: T(3),
      partial: true,
      transient: true,
    },
    {
      id: "prt_text1",
      seq: 3,
      kind: "assistant_message",
      role: "assistant",
      body: "starting",
      timestamp: T(4),
      partial: true,
    },
    {
      id: "prt_tool_a",
      seq: 4,
      kind: "tool_call",
      tool_name: "bash",
      call_id: "call_00_a",
      input: '{"command":"true"}',
      output: settled ? "ok" : undefined,
      status: settled ? "completed" : "running",
      partial: settled ? undefined : true,
      timestamp: T(5),
    },
    {
      id: "prt_tool_b",
      seq: 5,
      kind: "tool_call",
      tool_name: "bash",
      call_id: "call_00_b",
      input: '{"command":"boom"}',
      output: settled ? "Error: ENOENT: no such file or directory" : undefined,
      status: settled ? "failed" : "pending",
      partial: settled ? undefined : true,
      timestamp: T(6),
    },
    {
      id: "prt_text2",
      seq: 6,
      kind: "assistant_message",
      role: "assistant",
      body: "between tools",
      timestamp: T(7),
      partial: true,
    },
  ];
}

function daemonConversation(events: OpenCodeDaemonEvent[]) {
  return {
    available: true,
    session_id: "ses_up",
    activity: {
      id: "ses_up:activity:call_00_a",
      status: events.some(
        (event) =>
          event.kind === "tool_call" &&
          (event.partial === true ||
            event.status === "running" ||
            event.status === "pending"),
      )
        ? "running"
        : "completed",
      started_at: T(2),
    },
    events,
  };
}

describe("OpenCode Interface tool-call projection", () => {
  test("regression: daemon tool_call/reasoning events survive normalization as tool/commentary", () => {
    const conversation = normalizeCodexConversation(
      daemonConversation(openCodeDaemonEvents(false)),
    );

    // Before the fix, normalizeKind rejected "tool_call" and "reasoning", so
    // every OpenCode tool card silently disappeared while text still rendered.
    const toolEvents = conversation.events.filter((event) => event.kind === "tool");
    const reasoningEvents = conversation.events.filter(
      (event) => event.kind === "commentary",
    );
    expect(toolEvents).toHaveLength(2);
    expect(reasoningEvents).toHaveLength(1);

    expect(toolEvents[0]).toMatchObject({
      id: "prt_tool_a",
      kind: "tool",
      tool_name: "bash",
      call_id: "call_00_a",
      input: '{"command":"true"}',
      status: "running",
      partial: true,
    });
    expect(toolEvents[1]).toMatchObject({
      id: "prt_tool_b",
      status: "pending",
      partial: true,
    });
    expect(reasoningEvents[0]).toMatchObject({
      id: "prt_reason",
      kind: "commentary",
      body: "thinking hard",
    });
  });

  test("corrected projection: Interface renders tool cards and reasoning for OpenCode", () => {
    const inFlight = normalizeCodexConversation(
      daemonConversation(openCodeDaemonEvents(false)),
    );
    const inFlightItems = projectZenTimeline(inFlight.events, null).items;
    const inFlightTools = inFlightItems.filter(
      (item) => item.type === "activity" && item.id?.startsWith("prt_tool"),
    );
    expect(inFlightTools).toHaveLength(2);
    expect(inFlightTools[0]).toMatchObject({ id: "prt_tool_a", tone: "running" });
    expect(inFlightTools[1]).toMatchObject({ id: "prt_tool_b", tone: "running" });

    const reasoningItem = inFlightItems.find((item) => item.id === "prt_reason");
    expect(reasoningItem).toMatchObject({
      type: "activity",
      activityKind: "reasoning",
      body: "thinking hard",
    });

    const settled = normalizeCodexConversation(
      daemonConversation(openCodeDaemonEvents(true)),
    );
    const settledItems = projectZenTimeline(settled.events, null).items;
    const settledToolA = settledItems.find((item) => item.id === "prt_tool_a");
    const settledToolB = settledItems.find((item) => item.id === "prt_tool_b");
    expect(settledToolA).toMatchObject({ type: "activity", tone: "success" });
    expect(settledToolB).toMatchObject({ type: "activity", tone: "failed" });

    const texts = settledItems.filter((item) => item.type === "message");
    expect(texts.map((item) => item.id)).toEqual(["prt_user", "prt_text1", "prt_text2"]);
  });

  test("unknown kinds fail closed without dropping surrounding messages", () => {
    const events = openCodeDaemonEvents(true);
    const conversation = normalizeCodexConversation(
      daemonConversation([
        ...events.slice(0, 3),
        { id: "mystery", seq: 7, kind: "mystery_kind", body: "noise" },
        ...events.slice(3),
      ]),
    );
    expect(conversation.events.some((event) => event.id === "mystery")).toBe(false);
    expect(conversation.events).toHaveLength(events.length);
  });

  test("restarted snapshot/upsert keeps exact stable identity without duplicates", () => {
    const poll1 = normalizeCodexConversation(
      daemonConversation(openCodeDaemonEvents(false)),
    );
    const poll2 = normalizeCodexConversation(
      daemonConversation(openCodeDaemonEvents(true)),
    );
    const reconciled = reconcileConversationDeltaEvents(poll1.events, poll2.events);
    const byId = new Map(reconciled.map((event) => [event.id, event]));
    expect(byId.size).toBe(reconciled.length);
    expect(byId.size).toBe(6);
    expect(
      projectZenTimeline(reconciled, null).items.filter((item) =>
        item.id?.startsWith("prt_tool"),
      ),
    ).toHaveLength(2);

    const toolA = byId.get("prt_tool_a");
    expect(toolA).toMatchObject({
      kind: "tool",
      status: "completed",
      output: "ok",
    });
    const toolB = byId.get("prt_tool_b");
    expect(toolB).toMatchObject({
      status: "failed",
      output: "Error: ENOENT: no such file or directory",
    });
    // Settled daemon events omit partial (Go omitempty), which must not keep
    // the tool card streaming.
    expect(byId.get("prt_tool_a")?.partial).toBeFalsy();
  });
});
