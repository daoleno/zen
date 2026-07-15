// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  normalizeCodexConversation,
  type StructuredTurn,
} from "../../services/codexConversation";
import { isCodexRequestRunning } from "./CodexChatControllerModel";
import {
  buildZenTimeline,
  mergePendingUserMessagesIntoTimeline,
  mergeWorkingTurnIntoTimeline,
} from "./CodexTimelineModel";
import {
  reconcileConversationSnapshot,
  reconcileConversationSyncLifecycle,
  reconcileStructuredLifecycleProjection,
  reconcileStructuredTurn,
  reconcileStructuredTurnQueue,
} from "./codexConversationReconciliation";
import {
  codexChatSessionCacheKey,
  resolveWorkingStructuredTurn,
  structuredConversationClientIdentity,
} from "./structuredTurnLifecycle";

const START = "2026-07-15T01:00:00.000Z";
const LATER = "2026-07-15T01:00:01.000Z";

function turn(
  status: StructuredTurn["status"] = "running",
  id: string = "turn-a",
  startedAt: string = START,
): StructuredTurn {
  return {
    id,
    status,
    started_at: startedAt,
    ...(status === "completed" ||
    status === "failed" ||
    status === "interrupted" ||
    status === "cancelled"
      ? { settled_at: LATER }
      : {}),
  };
}

describe("provider-neutral structured Working", () => {
  for (const source of [
    "codex_rollout",
    "claude_code_transcript",
    "cursor_transcript",
    "grok_session",
    "future_structured_provider",
  ]) {
    test(`${source} uses the same turn in Brain and Work`, () => {
      const conversation = normalizeCodexConversation({
        available: true,
        source,
        turn: turn(),
        events: [],
      });
      const surfaces = [
        {
          name: "brain",
          cacheKey: codexChatSessionCacheKey(
            "server-a",
            "host-a",
            "brain-thread:thread-a",
          ),
        },
        {
          name: "work",
          cacheKey: codexChatSessionCacheKey("server-a", "agent-a"),
        },
      ];
      expect(surfaces[0].cacheKey).toBe(
        "server-a:scope:brain-thread:thread-a",
      );
      expect(surfaces[1].cacheKey).toBe("server-a:agent:agent-a");
      for (const surface of surfaces) {
        const workingTurn = resolveWorkingStructuredTurn(
          conversation.turn,
          [],
        );
        expect(surface.name).toBeString();
        expect(surface.cacheKey).toBeString();
        expect(
          isCodexRequestRunning({ conversation: null, turn: workingTurn }),
        ).toBe(true);
        expect(
          mergeWorkingTurnIntoTimeline([], workingTurn).filter(
            (item) => item.type === "activity" && item.title === "Working",
          ),
        ).toHaveLength(1);
      }
    });
  }

  test("accepted non-queued input bridges pre-first-token Working", () => {
    const pending = {
      id: "pending-a",
      turnId: "turn-a",
      turnStartedAt: START,
      lifecycle: "sending",
    };
    expect(resolveWorkingStructuredTurn(undefined, [pending])).toBeUndefined();
    expect(
      resolveWorkingStructuredTurn(undefined, [
        { ...pending, acceptedAt: LATER },
      ]),
    ).toEqual(turn());
  });

  test("unacknowledged and accepted queued inputs never claim Working", () => {
    const pending = {
      id: "pending-b",
      turnId: "turn-b",
      turnStartedAt: LATER,
      acceptedAt: LATER,
      lifecycle: "queued",
    };
    expect(resolveWorkingStructuredTurn(undefined, [pending])).toBeUndefined();
  });

  test("silent reasoning/tool gaps keep one stable Working placeholder", () => {
    const events = [
      {
        id: "assistant-partial",
        seq: 1,
        timestamp: LATER,
        kind: "assistant_message",
        role: "assistant",
        body: "Partial answer",
        partial: true,
      },
      {
        id: "tool-running",
        seq: 2,
        timestamp: LATER,
        kind: "tool",
        title: "Searching",
        status: "running",
      },
    ];
    const once = mergeWorkingTurnIntoTimeline(buildZenTimeline(events), turn());
    const twice = mergeWorkingTurnIntoTimeline(once, turn());
    const placeholders = twice.filter(
      (item) => item.type === "activity" && item.title === "Working",
    );
    expect(placeholders).toHaveLength(1);
    expect(placeholders[0]?.id).toBe("working-turn:turn-a");
    expect(twice.at(-1)?.id).toBe("working-turn:turn-a");
  });

  test("Working stays before later queued messages without reordering the queue", () => {
    const providerTimeline = buildZenTimeline([
      {
        id: "user-a",
        seq: 1,
        timestamp: START,
        kind: "user_message",
        role: "user",
        body: "current turn",
      },
    ]);
    const withWorking = mergeWorkingTurnIntoTimeline(
      providerTimeline,
      turn(),
    );
    const composed = mergePendingUserMessagesIntoTimeline(withWorking, [
      {
        id: "pending-b",
        turnId: "turn-b",
        turnStartedAt: LATER,
        body: "queued second",
        sentText: "queued second",
        attachments: [],
        createdAt: LATER,
        lifecycle: "queued",
      },
      {
        id: "pending-c",
        turnId: "turn-c",
        turnStartedAt: "2026-07-15T01:00:02.000Z",
        body: "queued third",
        sentText: "queued third",
        attachments: [],
        createdAt: "2026-07-15T01:00:02.000Z",
        lifecycle: "queued",
      },
    ]);
    expect(composed.map((item) => item.id)).toEqual([
      "user-a",
      "working-turn:turn-a",
      "pending-b",
      "pending-c",
    ]);
  });

  test("current optimistic input precedes its Working anchor and queued inputs follow it", () => {
    const composed = mergePendingUserMessagesIntoTimeline(
      mergeWorkingTurnIntoTimeline([], turn()),
      [
        {
          id: "pending-a",
          turnId: "turn-a",
          turnStartedAt: START,
          body: "start",
          sentText: "start",
          attachments: [],
          createdAt: START,
          lifecycle: "sending",
          acceptedAt: START,
        },
        {
          id: "pending-b",
          turnId: "turn-b",
          turnStartedAt: LATER,
          body: "next",
          sentText: "next",
          attachments: [],
          createdAt: LATER,
          lifecycle: "queued",
          acceptedAt: LATER,
        },
      ],
    );
    expect(composed.map((item) => item.id)).toEqual([
      "pending-a",
      "working-turn:turn-a",
      "pending-b",
    ]);
  });

  test("current optimistic input stays ahead of assistant chunks that arrive before its echo", () => {
    const providerTimeline = buildZenTimeline([
      {
        id: "history",
        seq: 1,
        timestamp: "2026-07-15T00:59:00.000Z",
        kind: "assistant_message",
        role: "assistant",
        body: "Earlier answer",
      },
      {
        id: "assistant-current",
        seq: 2,
        timestamp: LATER,
        kind: "assistant_message",
        role: "assistant",
        body: "Current partial",
        partial: true,
      },
    ]);
    const composed = mergePendingUserMessagesIntoTimeline(
      mergeWorkingTurnIntoTimeline(providerTimeline, turn()),
      [{
        id: "pending-a",
        turnId: "turn-a",
        turnStartedAt: START,
        body: "current prompt",
        sentText: "current prompt",
        attachments: [],
        createdAt: START,
        lifecycle: "sending",
        acceptedAt: START,
        createdAfterEventIds: ["history"],
      }],
    );
    expect(composed.map((item) => item.id)).toEqual([
      "history",
      "pending-a",
      "assistant-current",
      "working-turn:turn-a",
    ]);
  });

  test("pre-ack input for a busy turn stays after the current Working anchor", () => {
    const composed = mergePendingUserMessagesIntoTimeline(
      mergeWorkingTurnIntoTimeline([], turn()),
      [{
        id: "pending-b",
        turnId: "turn-b",
        turnStartedAt: LATER,
        body: "next",
        sentText: "next",
        attachments: [],
        createdAt: LATER,
        lifecycle: "sending",
      }],
    );
    expect(composed.map((item) => item.id)).toEqual([
      "working-turn:turn-a",
      "pending-b",
    ]);
  });

  test("echoed queued input remains Queued after Working in authoritative order", () => {
    const providerTimeline = buildZenTimeline([
      {
        id: "user-a",
        seq: 1,
        timestamp: START,
        kind: "user_message",
        role: "user",
        body: "current",
      },
      {
        id: "echo-b",
        seq: 2,
        timestamp: LATER,
        kind: "user_message",
        role: "user",
        body: "queued",
      },
    ]);
    const composed = mergePendingUserMessagesIntoTimeline(
      mergeWorkingTurnIntoTimeline(providerTimeline, turn()),
      [{
        id: "pending-b",
        turnId: "turn-b",
        turnStartedAt: LATER,
        body: "queued",
        sentText: "queued",
        attachments: [],
        createdAt: LATER,
        lifecycle: "queued",
        acceptedAt: LATER,
        confirmedEventId: "echo-b",
      }],
    );
    expect(composed.map((item) => item.id)).toEqual([
      "user-a",
      "working-turn:turn-a",
      "pending-b",
    ]);
    expect(composed[2]).toMatchObject({
      pending: true,
      pendingLifecycle: "queued",
      pendingLifecycleLabel: "Queued next",
    });
  });

  test("partialness remains rendering-only after authoritative settlement", () => {
    const partial = {
      id: "assistant-partial",
      seq: 1,
      timestamp: LATER,
      kind: "assistant_message",
      role: "assistant",
      body: "Partial answer",
      partial: true,
    };
    expect(buildZenTimeline([partial])[0]).toMatchObject({ streaming: true });
    expect(
      isCodexRequestRunning({
        conversation: normalizeCodexConversation({
          available: true,
          turn: turn("completed"),
          events: [partial],
        }),
        events: [partial],
      }),
    ).toBe(false);
  });
});

describe("durable turn reconciliation", () => {
  test("fresh sync-status hydration carries a running turn and ordered queue without transcript events", () => {
    const queue = [
      turn("queued", "turn-b", LATER),
      turn("queued", "turn-c", "2026-07-15T01:00:02.000Z"),
    ];
    const hydrated = reconcileConversationSyncLifecycle(null, {
      active: true,
      turn: turn(),
      queued_turns: queue,
      reason: "session_not_ready",
    });
    expect(hydrated.events).toEqual([]);
    expect(hydrated.turn).toEqual(turn());
    expect(hydrated.queued_turns?.map((item) => item.id)).toEqual([
      "turn-b",
      "turn-c",
    ]);
    expect(resolveWorkingStructuredTurn(hydrated.turn, [])).toEqual(turn());
  });

  test("preserves identity/start across snapshot omission and same-ID updates", () => {
    const previous = normalizeCodexConversation({
      available: true,
      session_id: "thread-a",
      turn: turn(),
      events: [],
    });
    const reconnect = reconcileConversationSnapshot(
      previous,
      normalizeCodexConversation({
        available: true,
        session_id: "thread-a",
        events: [],
      }),
      true,
    );
    expect(reconnect.turn).toBe(previous.turn);

    const changedStart = reconcileStructuredTurn(
      reconnect.turn,
      turn("running", "turn-a", LATER),
    );
    expect(changedStart?.id).toBe("turn-a");
    expect(changedStart?.started_at).toBe(START);
  });

  test("same-ID terminal is sticky while a distinct newer-envelope turn replaces it", () => {
    const completed = reconcileStructuredTurn(turn(), turn("completed"));
    expect(completed?.status).toBe("completed");
    expect(reconcileStructuredTurn(completed, turn())?.status).toBe(
      "completed",
    );
    expect(
      reconcileStructuredTurn(
        completed,
        turn("running", "older-turn", "2026-07-15T00:59:00.000Z"),
      ),
    ).toMatchObject({
      id: "older-turn",
      status: "running",
      started_at: "2026-07-15T00:59:00.000Z",
    });
    expect(
      reconcileStructuredTurn(completed, turn("running", "turn-b", LATER)),
    ).toMatchObject({ id: "turn-b", status: "running", started_at: LATER });
  });

  test("future-skewed prior terminal cannot suppress a distinct current running turn", () => {
    const futureTerminal = turn(
      "completed",
      "future-terminal",
      "2026-07-16T01:00:00.000Z",
    );
    const currentRunning = turn("running", "real-current", START);
    expect(
      reconcileStructuredTurn(futureTerminal, currentRunning),
    ).toBe(currentRunning);
  });

  for (const status of [
    "completed",
    "failed",
    "interrupted",
    "cancelled",
  ] as const) {
    test(`${status} authoritatively settles Working`, () => {
      expect(resolveWorkingStructuredTurn(turn(status), [])).toBeUndefined();
    });
  }

  test("explicit empty queue clears while omission preserves oldest-first order", () => {
    const queue = [
      turn("queued", "turn-b", LATER),
      turn("queued", "turn-c", "2026-07-15T01:00:02.000Z"),
    ];
    expect(reconcileStructuredTurnQueue(queue, undefined)).toBe(queue);
    expect(reconcileStructuredTurnQueue(queue, [])).toEqual([]);
  });

  test("same-thread snapshot explicitly clears the authoritative queue", () => {
    const previous = normalizeCodexConversation({
      available: true,
      session_id: "thread-a",
      turn: turn(),
      queued_turns: [turn("queued", "turn-b", LATER)],
      events: [],
    });
    const snapshot = reconcileConversationSnapshot(
      previous,
      normalizeCodexConversation({
        available: true,
        session_id: "thread-a",
        turn: turn(),
        queued_turns: [],
        events: [],
      }),
      true,
    );
    expect(snapshot.queued_turns).toEqual([]);
  });

  test("a new daemon epoch clears stale pre-restart Working across snapshot, delta, and sync", () => {
    const previous = normalizeCodexConversation({
      available: true,
      session_id: "thread-a",
      turn: turn(),
      queued_turns: [turn("queued", "turn-b", LATER)],
      events: [],
    });
    const incoming = normalizeCodexConversation({
      available: true,
      session_id: "thread-a",
      turn_epoch: "daemon-b",
      turn_revision: 0,
      queued_turns: [],
      events: [],
    });

    const snapshot = reconcileConversationSnapshot(previous, incoming, true);
    expect(snapshot.turn).toBeUndefined();
    expect(snapshot.queued_turns).toEqual([]);

    const deltaLifecycle = reconcileStructuredLifecycleProjection(previous, {
      turn_epoch: "daemon-b",
      turn_revision: 0,
      queued_turns: [],
    });
    expect(deltaLifecycle.turn).toBeUndefined();
    expect(deltaLifecycle.queued_turns).toEqual([]);

    const sync = reconcileConversationSyncLifecycle(previous, {
      turn_epoch: "daemon-b",
      turn_revision: 0,
      queued_turns: [],
    });
    expect(sync.turn).toBeUndefined();
    expect(sync.queued_turns).toEqual([]);
  });

  test("same-epoch lower lifecycle revisions cannot regress Working or queue on reconnect", () => {
    const previous = normalizeCodexConversation({
      available: true,
      session_id: "thread-a",
      turn_epoch: "daemon-a",
      turn_revision: 10,
      turn: turn("running", "turn-current", START),
      queued_turns: [turn("queued", "turn-next", LATER)],
      events: [],
    });
    const stale = normalizeCodexConversation({
      available: true,
      session_id: "thread-a",
      turn_epoch: "daemon-a",
      turn_revision: 9,
      turn: turn("completed", "turn-old", "2026-07-15T00:59:00.000Z"),
      queued_turns: [],
      events: [],
    });

    const snapshot = reconcileConversationSnapshot(previous, stale, true);
    expect(snapshot.turn).toEqual(previous.turn);
    expect(snapshot.queued_turns).toEqual(previous.queued_turns);
    expect(snapshot.turn_revision).toBe(10);

    const deltaLifecycle = reconcileStructuredLifecycleProjection(previous, stale);
    expect(deltaLifecycle.turn).toEqual(previous.turn);
    expect(deltaLifecycle.queued_turns).toEqual(previous.queued_turns);
    expect(deltaLifecycle.turn_revision).toBe(10);

    const sync = reconcileConversationSyncLifecycle(previous, stale);
    expect(sync.turn).toEqual(previous.turn);
    expect(sync.queued_turns).toEqual(previous.queued_turns);
    expect(sync.turn_revision).toBe(10);
  });

  test("a settled turn waits for authoritative promotion of the next queue item", () => {
    const queued = {
      id: "pending-b",
      turnId: "turn-b",
      turnStartedAt: LATER,
      acceptedAt: LATER,
      lifecycle: "queued",
    };
    expect(
      resolveWorkingStructuredTurn(turn("completed"), [queued]),
    ).toBeUndefined();
    expect(
      resolveWorkingStructuredTurn(turn("running", "turn-b", LATER), [
        queued,
      ]),
    ).toMatchObject({ id: "turn-b", status: "running", started_at: LATER });
  });
});

describe("scope-first hydration identity", () => {
  test("Brain host replacement retains cache while Work remains agent-scoped", () => {
    expect(
      codexChatSessionCacheKey("server", "host-a", "brain:thread-a"),
    ).toBe(codexChatSessionCacheKey("server", "host-b", "brain:thread-a"));
    expect(codexChatSessionCacheKey("server", "work-a")).not.toBe(
      codexChatSessionCacheKey("server", "work-b"),
    );
  });

  test("thread-control identity uses stable session/path metadata and never cwd", () => {
    expect(
      structuredConversationClientIdentity({
        session_id: " session-a ",
        path: "/rollout/a.jsonl",
      }),
    ).toBe("session:session-a");
    expect(
      structuredConversationClientIdentity({ path: " /rollout/a.jsonl " }),
    ).toBe("path:/rollout/a.jsonl");
    expect(structuredConversationClientIdentity({ cwd: "/repo" })).toBeUndefined();
  });

  test("normalization rejects lifecycle records without a valid durable start", () => {
    expect(
      normalizeCodexConversation({
        available: true,
        turn: { id: "bad", status: "running" },
        events: [],
      }).turn,
    ).toBeUndefined();
    expect(
      normalizeCodexConversation({
        available: true,
        turn: { id: "bad", status: "running", started_at: "not-a-date" },
        events: [],
      }).turn,
    ).toBeUndefined();
  });
});
