import { describe, expect, test } from "bun:test";

import { normalizeCodexConversation } from "./codexConversation";
import { projectZenTimeline } from "../components/terminal/projectZenTimeline";
import {
  reconcileConversationDeltaEvents,
  reconcileConversationSnapshot,
} from "../components/terminal/interfaceConversationReconciliation";
import type { ZenTimelineItem } from "../components/terminal/InterfaceTimelineItemView";

const T = (sec: number) =>
  new Date(Date.parse("2026-08-08T00:00:10Z") + sec * 1000).toISOString();

/**
 * Daemon wire shape after the parser settle fix. OpenCode's real terminal
 * message carries finish "unknown" + time.completed; the parser settles the
 * turn and clears Partial on every event (Go omitempty), so a completed
 * reasoning part arrives with partial absent. A live turn keeps partial on the
 * reasoning part until the authoritative completion boundary lands.
 */
interface OpenCodeReasoningDaemonEvent {
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

function reasoningTurnDaemonEvents(live: boolean): OpenCodeReasoningDaemonEvent[] {
  const reasoning = {
    id: "prt_reason_final",
    seq: 4,
    kind: "reasoning",
    body: "Now I understand the scheduler",
    timestamp: T(4),
    transient: true,
  };
  return [
    {
      id: "prt_user",
      seq: 1,
      kind: "user_message",
      role: "user",
      body: "fix the scheduler",
      timestamp: T(1),
    },
    {
      id: "prt_reason_step1",
      seq: 2,
      kind: "reasoning",
      body: "first step thinking",
      timestamp: T(2),
      transient: true,
      partial: live ? true : undefined,
    },
    {
      id: "prt_tool",
      seq: 3,
      kind: "tool_call",
      tool_name: "bash",
      call_id: "call_1",
      input: '{"command":"go build"}',
      output: live ? undefined : "ok",
      status: live ? "running" : "completed",
      partial: live ? true : undefined,
      timestamp: T(3),
    },
    {
      ...reasoning,
      partial: live ? true : undefined,
    },
    {
      id: "prt_text",
      seq: 5,
      kind: "assistant_message",
      role: "assistant",
      body: "done",
      timestamp: T(5),
      partial: live ? true : undefined,
    },
  ];
}

function reasoningDaemonConversation(events: OpenCodeReasoningDaemonEvent[]) {
  const live = events.some(
    (event) => event.partial === true || event.status === "running",
  );
  return {
    available: true,
    session_id: "ses_rsn",
    activity: {
      id: "ses_rsn:activity:prt_user",
      status: live ? "running" : "completed",
      started_at: T(1),
      settled_at: live ? undefined : T(6),
    },
    events,
  };
}

function reasoningItem(
  items: ReturnType<typeof projectZenTimeline>["items"],
  id = "prt_reason_final",
) {
  return items.find(
    (item): item is Extract<ZenTimelineItem, { type: "activity" }> =>
      item.id === id && item.type === "activity",
  );
}

describe("OpenCode reasoning icon settle contract", () => {
  test("completed historical reasoning renders settled static state with loaded text", () => {
    const conversation = normalizeCodexConversation(
      reasoningDaemonConversation(reasoningTurnDaemonEvents(false)),
    );
    const { items } = projectZenTimeline(conversation.events, null);
    const reason = reasoningItem(items);
    expect(reason).toMatchObject({
      type: "activity",
      activityKind: "reasoning",
      tone: "neutral",
      icon: "bulb",
      body: "Now I understand the scheduler",
      defaultExpanded: false,
    });
    // Completed history must never keep the spinner source alive. The daemon
    // omits partial on settled rows (Go omitempty), so streaming is falsy.
    expect(reason?.streaming).toBeFalsy();
    expect(reason).not.toMatchObject({ tone: "running" });
  });

  test("active stream keeps the spinner only on the live reasoning row", () => {
    const conversation = normalizeCodexConversation(
      reasoningDaemonConversation(reasoningTurnDaemonEvents(true)),
    );
    const { items } = projectZenTimeline(conversation.events, null);
    expect(reasoningItem(items, "prt_reason_step1")).toMatchObject({
      tone: "running",
      streaming: true,
      defaultExpanded: true,
    });
    expect(reasoningItem(items)).toMatchObject({
      tone: "running",
      streaming: true,
      body: "Now I understand the scheduler",
    });
  });

  test("incremental terminal delta settles the exact reasoning row without duplicates", () => {
    const live = normalizeCodexConversation(
      reasoningDaemonConversation(reasoningTurnDaemonEvents(true)),
    );
    const settled = normalizeCodexConversation(
      reasoningDaemonConversation(reasoningTurnDaemonEvents(false)),
    );
    const deltaUpserts = settled.events.filter(
      (event) =>
        event.partial !==
        live.events.find((candidate) => candidate.id === event.id)?.partial ||
        event.status !==
        live.events.find((candidate) => candidate.id === event.id)?.status,
    );
    const reconciled = reconcileConversationDeltaEvents(
      live.events,
      deltaUpserts,
    );
    const byId = new Map(reconciled.map((event) => [event.id, event]));
    expect(byId.size).toBe(reconciled.length);
    expect(byId.size).toBe(5);
    const { items } = projectZenTimeline(reconciled, null);
    expect(reasoningItem(items, "prt_reason_step1")).toMatchObject({
      tone: "neutral",
    });
    expect(reasoningItem(items, "prt_reason_step1")?.streaming).toBeFalsy();
    expect(reasoningItem(items)).toMatchObject({
      tone: "neutral",
      body: "Now I understand the scheduler",
    });
    expect(reasoningItem(items)?.streaming).toBeFalsy();
    const reasoningRows = items.filter(
      (item) => item.type === "activity" && item.activityKind === "reasoning",
    );
    expect(reasoningRows).toHaveLength(2);
  });

  test("reconnect/cache revisit snapshot replacement settles completed history", () => {
    const live = normalizeCodexConversation(
      reasoningDaemonConversation(reasoningTurnDaemonEvents(true)),
    );
    const settled = normalizeCodexConversation(
      reasoningDaemonConversation(reasoningTurnDaemonEvents(false)),
    );
    const revisited = reconcileConversationSnapshot(live, settled, true);
    const { items } = projectZenTimeline(revisited.events, null);
    const ids = items.map((item) => item.id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(reasoningItem(items)).toMatchObject({
      tone: "neutral",
      body: "Now I understand the scheduler",
    });
    expect(reasoningItem(items)?.streaming).toBeFalsy();
  });

  test("tool interleaving stays correct on both live and settled projections", () => {
    const liveItems = projectZenTimeline(
      normalizeCodexConversation(
        reasoningDaemonConversation(reasoningTurnDaemonEvents(true)),
      ).events,
      null,
    ).items;
    expect(reasoningItem(liveItems, "prt_tool")).toMatchObject({
      tone: "running",
    });
    expect(reasoningItem(liveItems, "prt_tool")?.statusKey).toBe("running");

    const settledItems = projectZenTimeline(
      normalizeCodexConversation(
        reasoningDaemonConversation(reasoningTurnDaemonEvents(false)),
      ).events,
      null,
    ).items;
    expect(reasoningItem(settledItems, "prt_tool")).toMatchObject({
      tone: "success",
      statusKey: "completed",
    });
    expect(reasoningItem(settledItems)).toMatchObject({ tone: "neutral" });
  });

  test("cancelled/interrupted convergence settles while unresolved live parts stay running", () => {
    const interrupted = normalizeCodexConversation({
      ...reasoningDaemonConversation(reasoningTurnDaemonEvents(false)),
      activity: {
        id: "ses_rsn:activity:prt_user",
        status: "interrupted",
        started_at: T(1),
        settled_at: T(6),
      },
    });
    const interruptedItems = projectZenTimeline(interrupted.events, null).items;
    expect(reasoningItem(interruptedItems)).toMatchObject({
      tone: "neutral",
    });
    expect(reasoningItem(interruptedItems)?.streaming).toBeFalsy();

    // A genuinely unresolved live part (no terminal boundary) must keep the
    // spinner: fail-closed, never settled by text availability alone.
    const unresolved = normalizeCodexConversation(
      reasoningDaemonConversation([
        ...reasoningTurnDaemonEvents(true),
        {
          id: "prt_reason_open",
          seq: 6,
          kind: "reasoning",
          body: "still streaming thoughts",
          timestamp: T(7),
          transient: true,
          partial: true,
        },
      ]),
    );
    const unresolvedItems = projectZenTimeline(unresolved.events, null).items;
    expect(reasoningItem(unresolvedItems, "prt_reason_open")).toMatchObject({
      tone: "running",
      streaming: true,
      body: "still streaming thoughts",
    });
  });
});
