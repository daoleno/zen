import { describe, expect, test } from "bun:test";
import {
  isProviderActivityRunning,
  normalizeCodexConversation,
  type CodexConversation,
  type CodexConversationEvent,
} from "../../services/codexConversation";
import {
  shouldDropProviderChatNoiseEvent,
  shouldDropStructuredChatEvent,
} from "./interfaceConversationVisibility";
import { buildZenTimeline } from "./InterfaceTimelineModel";
import {
  reconcileConversationDeltaEvents,
  reconcileConversationSnapshot,
} from "./interfaceConversationReconciliation";

const STREAM_TIME = "2026-07-15T01:00:00Z";

function event(
  id: string,
  seq: number,
  overrides: Partial<CodexConversationEvent> = {},
): CodexConversationEvent {
  return {
    id,
    seq,
    timestamp: STREAM_TIME,
    kind: "assistant_message",
    role: "assistant",
    body: id,
    ...overrides,
  };
}

function conversation(events: CodexConversationEvent[]): CodexConversation {
  return {
    available: true,
    source: "provider_transcript",
    session_id: "thread-a",
    events,
  };
}

describe("incremental structured conversation streaming", () => {
  test("normalizes only boolean partial and transient state across the typed service boundary", () => {
    const normalized = normalizeCodexConversation({
      available: true,
      events: [
        event("streaming", 1, { partial: true, transient: true }),
        event("final", 2, { partial: false, transient: false }),
        { ...event("invalid", 3), partial: "true", transient: "true" },
      ],
    });

    expect(
      normalized.events.map(({ id, partial, transient }) => ({
        id,
        partial,
        transient,
      })),
    ).toEqual([
      { id: "streaming", partial: true, transient: true },
      { id: "final", partial: false, transient: false },
      { id: "invalid", partial: undefined, transient: undefined },
    ]);
  });

  test("multiple equal-time chunks upsert one logical message and finalization does not duplicate it", () => {
    let events = reconcileConversationDeltaEvents(
      [],
      [event("assistant:turn-7", 7, { body: "Hel", partial: true })],
    );
    events = reconcileConversationDeltaEvents(events, [
      event("assistant:turn-7", 7, { body: "Hello", partial: true }),
    ]);
    events = reconcileConversationDeltaEvents(events, [
      event("assistant:turn-7", 7, {
        body: "Hello world",
        partial: true,
      }),
    ]);

    expect(events).toHaveLength(1);
    expect(events[0]).toMatchObject({
      id: "assistant:turn-7",
      seq: 7,
      timestamp: STREAM_TIME,
      body: "Hello world",
      partial: true,
    });

    events = reconcileConversationDeltaEvents(events, [
      event("assistant:turn-7", 7, {
        body: "Hello world!",
        partial: false,
        status: "done",
      }),
    ]);
    events = reconcileConversationDeltaEvents(events, [
      event("ordinary:later", 8, {
        body: "A later ordinary message",
        partial: undefined,
      }),
    ]);

    expect(events.map((item) => item.id)).toEqual([
      "assistant:turn-7",
      "ordinary:later",
    ]);
    expect(
      events.filter((item) => item.id === "assistant:turn-7"),
    ).toHaveLength(1);
    expect(events[0]).toMatchObject({
      body: "Hello world!",
      partial: false,
      status: "done",
    });
  });

  test("same-thread snapshots are exact revisioned replacements", () => {
    const historical = event("history", 1, {
      body: "Durable history",
      partial: false,
    });
    const firstChunk = event("assistant:active", 2, {
      body: "In pro",
      partial: true,
    });
    const finalizedEphemeral = event("assistant:ephemeral", 3, {
      body: "Provider-only status",
      partial: false,
      transient: true,
      status: "done",
    });
    const previous = conversation([historical, firstChunk, finalizedEphemeral]);

    const reconnected = reconcileConversationSnapshot(
      previous,
      conversation([
        event("assistant:active", 2, {
          body: "In progress",
          partial: true,
        }),
      ]),
      true,
    );

    expect(reconnected.events.map((item) => item.id)).toEqual([
      "assistant:active",
    ]);
    expect(reconnected.events[0]).toMatchObject({
      body: "In progress",
      partial: true,
    });

    const rebuiltWithoutTransient = reconcileConversationSnapshot(
      reconnected,
      conversation([historical]),
      true,
    );
    expect(rebuiltWithoutTransient.events).toEqual([historical]);
  });

  test("authoritative delta deletes remove exactly the listed ids", () => {
    const finalized = event("finalized", 1, {
      body: "Keep me",
      partial: false,
    });
    const reasoning = event("reasoning:active", 2, {
      kind: "commentary",
      body: "Still reasoning",
      partial: true,
    });
    const later = event("ordinary:later", 3, {
      body: "Later message",
      partial: undefined,
    });
    const finalizedTransient = event("tool:ephemeral", 4, {
      kind: "tool",
      body: "Finished provider-only tool status",
      partial: false,
      transient: true,
      status: "done",
    });

    // The daemon reports deletions from its authoritative full-read diff, so
    // an explicit delete removes exactly that id; an upserted replacement
    // re-adds it.
    const reconciled = reconcileConversationDeltaEvents(
      [finalized, reasoning, later, finalizedTransient],
      [finalized],
      ["reasoning:active", "tool:ephemeral"],
    );

    expect(reconciled.map((item) => item.id)).toEqual([
      "finalized",
      "ordinary:later",
    ]);
  });

  test("transient streaming omissions stay monotonic without explicit deletes", () => {
    const reasoning = event("reasoning:active", 2, {
      kind: "commentary",
      body: "Still reasoning",
      partial: true,
    });
    const later = event("ordinary:later", 3, {
      body: "Later message",
      partial: undefined,
    });

    // A delta that simply omits events (no deletes listed) must never erase
    // them: only an explicit authoritative delete can remove history.
    const reconciled = reconcileConversationDeltaEvents(
      [reasoning, later],
      [later],
    );

    expect(reconciled.map((item) => item.id)).toEqual([
      "reasoning:active",
      "ordinary:later",
    ]);
  });

  test("partial content is rendering metadata and never owns Working lifecycle", () => {
    const partial = event("assistant:turn", 1, {
      body: "Answering",
      partial: true,
    });
    const finalized = { ...partial, partial: false, status: "done" };

    expect(isProviderActivityRunning(conversation([partial]).activity)).toBe(
      false,
    );
    expect(isProviderActivityRunning(conversation([finalized]).activity)).toBe(
      false,
    );
  });

  test("assistant and reasoning timeline items retain their live streaming state", () => {
    const assistant = event("assistant:turn", 1, {
      body: "A **partially",
      partial: true,
    });
    const reasoning = event("reasoning:turn", 2, {
      kind: "commentary",
      title: "Reasoning",
      body: "Checking `unfinished",
      partial: true,
    });

    const partialTimeline = buildZenTimeline([assistant, reasoning]);
    expect(partialTimeline[0]).toMatchObject({
      type: "message",
      id: "assistant:turn",
      role: "assistant",
      body: "A **partially",
      streaming: true,
    });
    expect(partialTimeline[1]).toMatchObject({
      type: "activity",
      id: "reasoning:turn",
      activityKind: "reasoning",
      body: "Checking `unfinished",
      streaming: true,
      tone: "running",
      defaultExpanded: true,
    });

    const finalTimeline = buildZenTimeline([
      { ...assistant, body: "A **complete** answer", partial: false },
      {
        ...reasoning,
        body: "Checking `complete`",
        partial: false,
        status: "done",
      },
    ]);
    expect(finalTimeline[0]).toMatchObject({
      id: "assistant:turn",
      body: "A **complete** answer",
      streaming: false,
    });
    expect(finalTimeline[1]).toMatchObject({
      id: "reasoning:turn",
      body: "Checking `complete`",
      streaming: false,
      tone: "neutral",
      defaultExpanded: false,
    });
  });

  test("Grok reasoning survives chat filtering while provider plan noise remains hidden", () => {
    const reasoning = event("grok:reasoning", 1, {
      kind: "commentary",
      title: "Reasoning",
      body: "A genuine native thought chunk",
      partial: true,
    });
    const plan = event("grok:plan", 2, {
      kind: "plan",
      role: undefined,
      body: undefined,
      title: "Updated Plan",
      plan: [{ step: "internal provider plan", status: "in_progress" }],
    });
    expect(
      shouldDropProviderChatNoiseEvent("grok_session", reasoning.kind),
    ).toBe(false);
    expect(shouldDropProviderChatNoiseEvent("grok_session", plan.kind)).toBe(
      true,
    );
  });

  test("provider command transcript events remain visible user rows", () => {
    const command = event("command:/status", 1, {
      kind: "user_message",
      role: "user",
      body: "/status",
      source: "codex_rollout",
    });
    expect(shouldDropStructuredChatEvent("codex_rollout", command)).toBe(false);
    expect(
      shouldDropStructuredChatEvent("codex_rollout", {
        ...command,
        source: "terminal_snapshot",
      }),
    ).toBe(true);
  });

  test("stream updates leave specialized Calendar ordering and presentation unchanged", () => {
    const calendarResult = event("calendar_result:item:run", 1, {
      timestamp: "2026-07-15T00:59:00Z",
      kind: "status",
      role: undefined,
      title: "Daily brief complete",
      body: "Created the morning brief.",
      status: "done",
      source: "calendar_result",
    });
    const partial = event("assistant:turn", 2, {
      body: "Following up",
      partial: true,
    });
    const before = buildZenTimeline([partial, calendarResult]);
    const after = buildZenTimeline([
      { ...partial, body: "Following up now", partial: false, status: "done" },
      calendarResult,
    ]);

    expect(before.map((item) => item.id)).toEqual([
      "calendar_result:item:run",
      "assistant:turn",
    ]);
    expect(after.map((item) => item.id)).toEqual([
      "calendar_result:item:run",
      "assistant:turn",
    ]);
    expect(before[0]).toEqual(after[0]);
    expect(after[0]).toMatchObject({
      type: "activity",
      title: "Daily brief complete",
      tone: "success",
      icon: "calendar-outline",
      detail: "Created the morning brief.",
      bodyKind: undefined,
    });
  });
});
