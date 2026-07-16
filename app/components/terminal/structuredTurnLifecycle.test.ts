import { describe, expect, test } from "bun:test";
import {
  normalizeCodexConversation,
  type CodexConversationEvent,
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

describe("provider-neutral structured Activity Working", () => {
  for (const source of [
    "codex_rollout",
    "claude_code_transcript",
    "cursor_transcript",
    "grok_session",
    "future_structured_provider",
  ]) {
    test(`${source} uses the same Activity in Brain and Work`, () => {
      const conversation = normalizeCodexConversation({
        available: true,
        source,
        activity: turn(),
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
        const workingTurn = resolveWorkingStructuredTurn(conversation.activity);
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

  test("accepted input cannot invent Working before a provider Activity fact", () => {
    expect(resolveWorkingStructuredTurn(undefined)).toBeUndefined();
  });

  test("terminal Activity clears Working even when five accepted Submissions remain", () => {
    const acceptedSubmissions = Array.from({ length: 5 }, (_, index) => ({
      id: `submission-${index + 1}`,
    }));

    expect(acceptedSubmissions).toHaveLength(5);
    expect(resolveWorkingStructuredTurn(turn("completed", "activity-one"))).toBeUndefined();
  });

  test("silent reasoning/tool gaps keep one stable Working placeholder", () => {
    const events: CodexConversationEvent[] = [
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

  test("Working remains a footer while queued metadata leaves Submission order intact", () => {
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
      "pending-b",
      "pending-c",
      "working-turn:turn-a",
    ]);
  });

  test("optimistic Submissions retain acceptance order before the Working footer", () => {
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
      "pending-b",
      "working-turn:turn-a",
    ]);
  });

  test("same-boundary duplicate text Submissions keep creation order", () => {
    const history = buildZenTimeline([{
      id: "history",
      seq: 1,
      timestamp: START,
      kind: "assistant_message",
      role: "assistant",
      body: "Earlier answer",
    }]);
    const repeated = (id: string) => ({
      id,
      turnId: id,
      turnStartedAt: LATER,
      body: "identical",
      sentText: "identical",
      attachments: [],
      createdAt: LATER,
      lifecycle: "queued" as const,
      createdAfterEventIds: ["history"],
    });
    const composed = mergePendingUserMessagesIntoTimeline(
      mergeWorkingTurnIntoTimeline(history, turn()),
      [repeated("submission-a"), repeated("submission-b"), repeated("submission-c")],
    );
    expect(composed.map((item) => item.id)).toEqual([
      "history",
      "submission-a",
      "submission-b",
      "submission-c",
      "working-turn:turn-a",
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

  test("pre-ack input is not relocated behind the Working footer", () => {
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
      "pending-b",
      "working-turn:turn-a",
    ]);
  });

  test("echoed queued input keeps canonical event identity and position", () => {
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
      "echo-b",
      "working-turn:turn-a",
    ]);
    expect(composed[1]).toMatchObject({
      pending: true,
      pendingLifecycle: "queued",
      pendingLifecycleLabel: "Queued next",
    });
  });

  test("partialness remains rendering-only after authoritative settlement", () => {
    const partial: CodexConversationEvent = {
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

describe("canonical Activity snapshots", () => {
  test("Codex legacy dispatch turn cannot resurrect visible Working", () => {
    expect(
      isCodexRequestRunning({
        conversation: normalizeCodexConversation({
          available: true,
          source: "codex_rollout",
          turn: turn("running"),
          events: [],
        }),
      }),
    ).toBe(false);
  });

  for (const status of [
    "completed",
    "failed",
    "interrupted",
    "cancelled",
  ] as const) {
    test(`${status} authoritatively settles Working`, () => {
      expect(resolveWorkingStructuredTurn(turn(status))).toBeUndefined();
    });
  }

  test("same-thread snapshot exactly clears Activity queue and events", () => {
    const previous = normalizeCodexConversation({
      available: true,
      session_id: "thread-a",
      activity: turn(),
      queued_turns: [turn("queued", "turn-b", LATER)],
      events: [{ id: "history", seq: 1, kind: "assistant_message", body: "old" }],
    });
    const snapshot = reconcileConversationSnapshot(
      previous,
      normalizeCodexConversation({
        available: true,
        session_id: "thread-a",
        events: [],
      }),
      true,
    );
    expect(snapshot.queued_turns).toEqual([]);
    expect(snapshot.activity).toBeUndefined();
    expect(snapshot.events).toEqual([]);
  });

  test("snapshot replacement never merges a lower legacy lifecycle revision", () => {
    const previous = normalizeCodexConversation({
      available: true,
      session_id: "thread-a",
      activity: turn("running", "activity-current", START),
      queued_turns: [turn("queued", "turn-b", LATER)],
      events: [],
    });
    const replacement = normalizeCodexConversation({
      available: true,
      session_id: "thread-a",
      turn_epoch: "daemon-a",
      turn_revision: 9,
      activity: turn("completed", "activity-old", "2026-07-15T00:59:00.000Z"),
      queued_turns: [],
      events: [],
    });

    const snapshot = reconcileConversationSnapshot(previous, replacement, true);
    expect(snapshot.activity).toEqual(replacement.activity);
    expect(snapshot.turn_revision).toBe(9);
    expect(snapshot.queued_turns).toEqual([]);
  });

  test("only a successor provider Activity can reopen Working", () => {
    expect(resolveWorkingStructuredTurn(turn("completed"))).toBeUndefined();
    expect(
      resolveWorkingStructuredTurn(turn("running", "turn-b", LATER)),
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
