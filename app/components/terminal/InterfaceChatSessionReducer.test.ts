import { describe, expect, mock, test } from "bun:test";
import type { ProviderActivity } from "../../services/codexConversation";
import type { PendingUserMessage } from "./InterfaceChatSession";

mock.module("react-native", () => ({ Platform: { OS: "web" } }));
mock.module("../../services/auth", () => ({
  buildAuthorizationHeader: async () => "test-authorization",
}));
mock.module("../../services/connectionIssue", () => ({
  diagnoseConnectionIssue: async () => null,
}));

const { interfaceChatThreadReducer, interfaceConversationStreamErrorMessage } =
  await import("./InterfaceChatSession");

function pending(
  overrides: Partial<PendingUserMessage> = {},
): PendingUserMessage {
  return {
    id: "pending-a",
    body: "hello",
    sentText: "hello",
    attachments: [],
    createdAt: "2026-07-17T10:00:00.000Z",
    lifecycle: "pending",
    dispatchRequestId: "request-a",
    dispatchAttemptOrder: 1,
    createdAfterMaxSeq: 3,
    createdAfterEventIds: ["history"],
    ...overrides,
  };
}

function state(
  pendingUserMessages: PendingUserMessage[],
): Parameters<typeof interfaceChatThreadReducer>[0] {
  return {
    cacheKey: "server-a:agent:agent-a",
    conversation: null,
    loading: false,
    error: null,
    pendingUserMessages,
    turnFocusAnchorAliases: new Map(),
    streamCursor: { revision: 7, generation: 1 },
    awaitingSnapshot: false,
    resyncToken: 0,
  };
}

function activity(
  status: ProviderActivity["status"] = "running",
): ProviderActivity {
  return {
    id: "provider-activity-a",
    status,
    started_at: "2026-07-17T10:00:00.000Z",
    ...(status === "running" ? {} : { settled_at: "2026-07-17T10:00:02.000Z" }),
  };
}

function streamingState(currentActivity?: ProviderActivity) {
  return {
    ...state([]),
    conversation: {
      available: true,
      session_id: "thread-a",
      activity: currentActivity,
      events: [],
    },
    streamCursor: {
      requestId: "stream-a",
      conversationId: "thread-a",
      revision: 7,
      generation: 1,
    },
  };
}

describe("process-local Chat reducer", () => {
  test("uses provider-neutral copy when a stream error has no message", () => {
    expect(interfaceConversationStreamErrorMessage(new Error(""))).toBe(
      "Could not stream this conversation.",
    );
    expect(
      interfaceConversationStreamErrorMessage(
        new Error("Provider supplied detail"),
      ),
    ).toBe("Provider supplied detail");
  });

  test("retains every live local row without destructive count truncation", () => {
    const existing = Array.from({ length: 15 }, (_, index) =>
      pending({ id: `pending-${index}` }),
    );
    const current = state(existing);
    const next = interfaceChatThreadReducer(current, {
      type: "add_pending_user_message",
      message: {
        id: "pending-15",
        body: "new",
        sentText: "new",
        attachments: [],
        createdAt: "2026-07-17T10:00:15.000Z",
        lifecycle: "pending",
        dispatchRequestId: "request-15",
      },
    });
    expect(next.pendingUserMessages.map((message) => message.id)).toEqual([
      ...existing.map((message) => message.id),
      "pending-15",
    ]);
    expect(next.turnFocusAnchorAliases).toBe(current.turnFocusAnchorAliases);
  });

  test("the reducer captures the latest provider boundary for a new local row", () => {
    const current = {
      ...state([]),
      conversation: {
        available: true,
        events: [
          { id: "provider-1", seq: 3, kind: "assistant_message" as const },
          { id: "provider-2", seq: 5, kind: "user_message" as const },
        ],
      },
    };
    const next = interfaceChatThreadReducer(current, {
      type: "add_pending_user_message",
      message: {
        id: "pending-new",
        body: "new",
        sentText: "new",
        attachments: [],
        createdAt: "2026-07-17T10:00:01.000Z",
        lifecycle: "pending",
        dispatchRequestId: "request-new",
      },
    });
    expect(next.pendingUserMessages[0]).toMatchObject({
      id: "pending-new",
      createdAfterMaxSeq: 5,
      createdAfterEventIds: ["provider-1", "provider-2"],
    });
  });

  test("connection loss and a same-key snapshot without an echo preserve uncertain local truth", () => {
    const providerFocused = {
      id: "provider-focused",
      seq: 4,
      kind: "user_message" as const,
      body: "already canonical",
    };
    const localRows = [
      pending({
        dispatchAttemptOrder: 3,
        createdAfterMaxSeq: 4,
        createdAfterEventIds: ["provider-focused"],
      }),
    ];
    const aliases = new Map([["provider-focused", "pending-focused"]]);
    const disconnected = interfaceChatThreadReducer(
      {
        ...streamingState(),
        conversation: {
          ...streamingState().conversation!,
          events: [providerFocused],
        },
        pendingUserMessages: localRows,
        turnFocusAnchorAliases: aliases,
      },
      { type: "stream_error", error: "connection_closed", generation: 1 },
    );
    const restarted = interfaceChatThreadReducer(disconnected, {
      type: "stream_start",
      generation: 2,
    });

    expect(restarted.pendingUserMessages).toBe(localRows);
    expect(restarted.turnFocusAnchorAliases).toBe(aliases);
    expect(restarted.error).toBeNull();
    expect(restarted.streamCursor).toEqual({
      conversationId: "thread-a",
      revision: 0,
      generation: 2,
    });

    const accepted = interfaceChatThreadReducer(restarted, {
      type: "snapshot",
      generation: 2,
      payload: {
        request_id: "stream-b",
        conversation_id: "thread-a",
        revision: 8,
        conversation: {
          available: true,
          session_id: "thread-a",
          events: [
            providerFocused,
            {
              id: "provider-assistant",
              seq: 10,
              kind: "assistant_message",
              body: "still no provider echo",
            },
          ],
        },
      },
    });

    expect(accepted.pendingUserMessages.map((message) => message.id)).toEqual([
      "pending-a",
    ]);
    expect(accepted.pendingUserMessages[0]).toBe(localRows[0]);
    expect(accepted.turnFocusAnchorAliases).toBe(aliases);
  });

  test("connection loss and a same-key snapshot reconcile the eventual provider echo", () => {
    const localRows = [pending()];
    const disconnected = interfaceChatThreadReducer(
      {
        ...streamingState(),
        pendingUserMessages: localRows,
      },
      { type: "stream_error", error: "connection_closed", generation: 1 },
    );
    const restarted = interfaceChatThreadReducer(disconnected, {
      type: "stream_start",
      generation: 2,
    });
    expect(restarted.pendingUserMessages).toBe(localRows);

    const accepted = interfaceChatThreadReducer(restarted, {
      type: "snapshot",
      generation: 2,
      payload: {
        request_id: "stream-b",
        conversation_id: "thread-a",
        revision: 8,
        conversation: {
          available: true,
          session_id: "thread-a",
          events: [
            {
              id: "provider-echo-a",
              seq: 11,
              kind: "user_message",
              body: "provider canonical body",
            },
          ],
        },
      },
    });

    expect(accepted.pendingUserMessages).toEqual([]);
    expect([...accepted.turnFocusAnchorAliases]).toEqual([
      ["provider-echo-a", "pending-a"],
    ]);
    expect(accepted.conversation?.events[0]).toMatchObject({
      id: "provider-echo-a",
      body: "provider canonical body",
    });
  });

  test("the first accepted different-conversation snapshot clears local projections", () => {
    const providerFocused = {
      id: "provider-focused",
      seq: 4,
      kind: "user_message" as const,
      body: "already canonical",
    };
    const localRows = [
      pending({
        dispatchAttemptOrder: 3,
        createdAfterMaxSeq: 4,
        createdAfterEventIds: ["provider-focused"],
      }),
    ];
    const aliases = new Map([["provider-focused", "pending-focused"]]);
    const disconnected = interfaceChatThreadReducer(
      {
        ...streamingState(),
        conversation: {
          ...streamingState().conversation!,
          events: [providerFocused],
        },
        pendingUserMessages: localRows,
        turnFocusAnchorAliases: aliases,
      },
      { type: "stream_error", error: "connection_closed", generation: 1 },
    );
    const restarted = interfaceChatThreadReducer(disconnected, {
      type: "stream_start",
      generation: 2,
    });
    expect(restarted.pendingUserMessages).toBe(localRows);
    expect(restarted.turnFocusAnchorAliases).toBe(aliases);

    const accepted = interfaceChatThreadReducer(restarted, {
      type: "snapshot",
      generation: 2,
      payload: {
        request_id: "stream-b",
        conversation_id: "thread-b",
        revision: 1,
        conversation: {
          available: true,
          session_id: "thread-b",
          events: [],
        },
      },
    });

    expect(accepted.pendingUserMessages).toEqual([]);
    expect(accepted.turnFocusAnchorAliases.size).toBe(0);
    expect(accepted.conversation?.session_id).toBe("thread-b");
  });

  test("the first subscription does not erase a row sent before effects ran", () => {
    const current = {
      ...state([pending()]),
      streamCursor: { revision: 0 },
    };
    const next = interfaceChatThreadReducer(current, {
      type: "stream_start",
      generation: 1,
    });
    expect(next.pendingUserMessages).toBe(current.pendingUserMessages);
  });

  test("a key round trip starts clean instead of restoring an old provider projection", () => {
    const acceptedA = interfaceChatThreadReducer(streamingState(), {
      type: "snapshot",
      generation: 1,
      payload: {
        request_id: "stream-a",
        conversation_id: "thread-a",
        revision: 8,
        conversation: {
          available: true,
          session_id: "thread-a",
          activity: activity(),
          events: [
            {
              id: "provider-event-a",
              seq: 1,
              kind: "assistant_message",
              body: "old provider output",
            },
          ],
        },
      },
    });
    expect(acceptedA.conversation).toMatchObject({
      session_id: "thread-a",
      activity: activity(),
      events: [{ id: "provider-event-a" }],
    });

    const keyB = interfaceChatThreadReducer(
      {
        ...acceptedA,
        turnFocusAnchorAliases: new Map([["provider-user-a", "pending-a"]]),
      },
      {
        type: "cache_key_changed",
        cacheKey: "server-a:agent:agent-b",
      },
    );
    expect(keyB.turnFocusAnchorAliases.size).toBe(0);
    const returnedA = interfaceChatThreadReducer(keyB, {
      type: "cache_key_changed",
      cacheKey: "server-a:agent:agent-a",
    });

    expect(returnedA).toMatchObject({
      cacheKey: "server-a:agent:agent-a",
      conversation: null,
      loading: true,
      error: null,
      pendingUserMessages: [],
      awaitingSnapshot: false,
      resyncToken: 0,
    });
    expect(returnedA.conversation?.events).toBeUndefined();
    expect(returnedA.conversation?.session_id).toBeUndefined();
    expect(returnedA.conversation?.activity).toBeUndefined();
    expect(returnedA.streamCursor).toEqual({ revision: 0 });
  });

  test("a same-key stream restart preserves the mounted conversation and Activity", () => {
    const accepted = interfaceChatThreadReducer(streamingState(), {
      type: "snapshot",
      generation: 1,
      payload: {
        request_id: "stream-a",
        conversation_id: "thread-a",
        revision: 8,
        conversation: {
          available: true,
          session_id: "thread-a",
          activity: activity(),
          events: [
            {
              id: "provider-event-a",
              seq: 1,
              kind: "assistant_message",
              body: "current provider output",
            },
          ],
        },
      },
    });

    const restarted = interfaceChatThreadReducer(accepted, {
      type: "stream_start",
      generation: 2,
    });

    expect(restarted.cacheKey).toBe(accepted.cacheKey);
    expect(restarted.conversation).toBe(accepted.conversation);
    expect(restarted.conversation?.activity).toBe(
      accepted.conversation?.activity,
    );
    expect(restarted.loading).toBe(false);
    expect(restarted.awaitingSnapshot).toBe(true);
    expect(restarted.streamCursor).toEqual({
      conversationId: "thread-a",
      revision: 0,
      generation: 2,
    });
  });

  test("a stale failure cannot overwrite the reducer's current attempt", () => {
    const current = state([pending({ dispatchRequestId: "request-current" })]);
    const stale = interfaceChatThreadReducer(current, {
      type: "reject_pending_user_message",
      id: "pending-a",
      requestId: "request-previous",
      code: "old_failure",
      message: "old attempt",
    });
    expect(stale).toBe(current);

    const failed = interfaceChatThreadReducer(current, {
      type: "reject_pending_user_message",
      id: "pending-a",
      requestId: "request-current",
      code: "send_input_failed",
      message: "provider refused input",
    });
    expect(failed.pendingUserMessages[0]).toMatchObject({
      lifecycle: "failed",
      dispatchRequestId: "request-current",
      failureMessage: "provider refused input",
    });
  });

  test("manual retry updates one row in place as a fresh attempt", () => {
    const rows = [
      pending({
        id: "first",
        lifecycle: "failed",
        dispatchAttemptOrder: 1,
      }),
      pending({
        id: "second",
        lifecycle: "failed",
        dispatchAttemptOrder: 2,
      }),
    ];
    const next = interfaceChatThreadReducer(state(rows), {
      type: "begin_pending_user_message_attempt",
      id: "first",
      requestId: "request-new",
    });
    expect(next.pendingUserMessages.map((message) => message.id)).toEqual([
      "first",
      "second",
    ]);
    expect(next.pendingUserMessages[0]).toMatchObject({
      lifecycle: "pending",
      dispatchRequestId: "request-new",
      dispatchAttemptOrder: 3,
    });
    expect(next.pendingUserMessages[1]).toBe(rows[1]);
  });

  test("a successful new row bounds aliases while Retry preserves the current one", () => {
    const aliased = {
      ...state([pending({ lifecycle: "failed" })]),
      turnFocusAnchorAliases: new Map([
        ["provider-current", "pending-current"],
      ]),
    };
    const retried = interfaceChatThreadReducer(aliased, {
      type: "begin_pending_user_message_attempt",
      id: "pending-a",
      requestId: "request-retry",
    });
    expect(retried.turnFocusAnchorAliases).toBe(aliased.turnFocusAnchorAliases);

    const superseded = interfaceChatThreadReducer(retried, {
      type: "add_pending_user_message",
      message: {
        id: "pending-new",
        body: "new",
        sentText: "new",
        attachments: [],
        createdAt: "2026-07-17T10:00:02.000Z",
        lifecycle: "pending",
        dispatchRequestId: "request-new",
      },
    });
    expect(superseded.turnFocusAnchorAliases.size).toBe(0);
  });

  test("provider echo reconciliation retains only a causal presentation alias", () => {
    const current = {
      ...streamingState(),
      pendingUserMessages: [pending()],
    };
    const next = interfaceChatThreadReducer(current, {
      type: "snapshot",
      generation: 1,
      payload: {
        request_id: "stream-a",
        conversation_id: "thread-a",
        revision: 8,
        conversation: {
          available: true,
          session_id: "thread-a",
          events: [
            {
              id: "provider-echo-a",
              seq: 11,
              kind: "user_message",
              body: "provider canonical body",
            },
          ],
        },
      },
    });

    expect(next.pendingUserMessages).toEqual([]);
    expect(next.turnFocusAnchorAliases.get("provider-echo-a")).toBe(
      "pending-a",
    );
    expect(next.conversation?.events[0]).toMatchObject({
      id: "provider-echo-a",
      body: "provider canonical body",
    });
  });

  test("incremental retry echoes keep the rapid-send focus alias bounded to B", () => {
    const current = {
      ...streamingState(),
      pendingUserMessages: [
        pending({
          id: "pending-a",
          dispatchRequestId: "request-a-retry",
          dispatchAttemptOrder: 3,
        }),
        pending({
          id: "pending-b",
          dispatchRequestId: "request-b",
          dispatchAttemptOrder: 2,
        }),
      ],
    };
    const providerB = {
      id: "provider-b",
      seq: 11,
      kind: "user_message" as const,
      body: "B from provider",
    };
    const afterB = interfaceChatThreadReducer(current, {
      type: "snapshot",
      generation: 1,
      payload: {
        request_id: "stream-a",
        conversation_id: "thread-a",
        revision: 8,
        conversation: {
          available: true,
          session_id: "thread-a",
          events: [providerB],
        },
      },
    });
    expect(afterB.pendingUserMessages.map((message) => message.id)).toEqual([
      "pending-a",
    ]);
    expect([...afterB.turnFocusAnchorAliases]).toEqual([
      ["provider-b", "pending-b"],
    ]);

    const afterA = interfaceChatThreadReducer(afterB, {
      type: "snapshot",
      generation: 1,
      payload: {
        request_id: "stream-a",
        conversation_id: "thread-a",
        revision: 9,
        conversation: {
          available: true,
          session_id: "thread-a",
          events: [
            providerB,
            {
              id: "provider-a-retry",
              seq: 12,
              kind: "user_message",
              body: "retried A from provider",
            },
          ],
        },
      },
    });
    expect(afterA.pendingUserMessages).toEqual([]);
    expect([...afterA.turnFocusAnchorAliases]).toEqual([
      ["provider-b", "pending-b"],
    ]);
  });

  test("snapshot replacement publishes running and terminal Activity exactly", () => {
    const running = interfaceChatThreadReducer(streamingState(), {
      type: "snapshot",
      generation: 1,
      payload: {
        request_id: "stream-a",
        conversation_id: "thread-a",
        revision: 8,
        conversation: {
          available: true,
          session_id: "thread-a",
          activity: activity(),
          events: [],
        },
      },
    });
    expect(running.conversation?.activity).toEqual(activity());

    const terminal = interfaceChatThreadReducer(running, {
      type: "snapshot",
      generation: 1,
      payload: {
        request_id: "stream-a",
        conversation_id: "thread-a",
        revision: 9,
        conversation: {
          available: true,
          session_id: "thread-a",
          activity: activity("completed"),
          events: [],
        },
      },
    });
    expect(terminal.conversation?.activity).toEqual(activity("completed"));
  });

  test("delta distinguishes absent Activity from terminal update and explicit clear", () => {
    const base = streamingState(activity());
    const absent = interfaceChatThreadReducer(base, {
      type: "delta",
      generation: 1,
      delta: {
        request_id: "stream-a",
        conversation_id: "thread-a",
        base_revision: 7,
        revision: 8,
        upserts: [],
        deletes: [],
      },
    });
    expect(absent.conversation?.activity).toBe(base.conversation?.activity);

    const terminal = interfaceChatThreadReducer(absent, {
      type: "delta",
      generation: 1,
      delta: {
        request_id: "stream-a",
        conversation_id: "thread-a",
        base_revision: 8,
        revision: 9,
        activity: activity("failed"),
        upserts: [],
        deletes: [],
      },
    });
    expect(terminal.conversation?.activity).toEqual(activity("failed"));

    const cleared = interfaceChatThreadReducer(terminal, {
      type: "delta",
      generation: 1,
      delta: {
        request_id: "stream-a",
        conversation_id: "thread-a",
        base_revision: 9,
        revision: 10,
        activity: null,
        upserts: [],
        deletes: [],
      },
    });
    expect(cleared.conversation?.activity).toBeUndefined();
  });

  test("sync status and local optimistic input never create or mutate Activity", () => {
    const current = streamingState(activity());
    const synced = interfaceChatThreadReducer(current, {
      type: "sync_status",
      generation: 1,
      status: {
        request_id: "stream-a",
        conversation_id: "thread-a",
        revision: 8,
        state: "ready",
      },
    });
    expect(synced.conversation?.activity).toBe(current.conversation?.activity);

    const local = interfaceChatThreadReducer(streamingState(), {
      type: "add_pending_user_message",
      message: {
        id: "local-only",
        body: "hello",
        sentText: "hello",
        attachments: [],
        createdAt: "2026-07-17T10:00:03.000Z",
        lifecycle: "pending",
        dispatchRequestId: "request-local",
      },
    });
    expect(local.conversation?.activity).toBeUndefined();
  });

  test("Brain admission user_message clears Pending and survives empty restart snapshot", () => {
    const requestId = "app-request-pending-1";
    const admissionId = requestId;
    const body = "accepted user body for pending clear";
    const withPending = interfaceChatThreadReducer(streamingState(), {
      type: "add_pending_user_message",
      message: pending({
        id: "local-pending",
        body,
        sentText: body,
        dispatchRequestId: requestId,
        createdAfterEventIds: ["history"],
      }),
    });
    expect(withPending.pendingUserMessages).toHaveLength(1);

    const admitted = interfaceChatThreadReducer(withPending, {
      type: "snapshot",
      generation: 1,
      payload: {
        request_id: "stream-a",
        conversation_id: "thread-a",
        revision: 9,
        conversation: {
          available: true,
          session_id: "brain-thread:thread-a",
          events: [
            { id: "history", seq: 1, kind: "user_message", body: "earlier" },
            { id: admissionId, seq: 2, kind: "user_message", body },
            {
              id: "assistant-1",
              seq: 3,
              kind: "assistant_message",
              body: "assistant reply after cwd vanished",
            },
          ],
        },
      },
    });
    expect(admitted.pendingUserMessages).toEqual([]);
    expect(admitted.conversation?.events.map((event) => event.id)).toEqual([
      "history",
      admissionId,
      "assistant-1",
    ]);

    const disconnected = interfaceChatThreadReducer(admitted, {
      type: "stream_error",
      error: "connection_closed",
      generation: 1,
    });
    const restarted = interfaceChatThreadReducer(disconnected, {
      type: "stream_start",
      generation: 2,
    });
    const afterRestart = interfaceChatThreadReducer(restarted, {
      type: "snapshot",
      generation: 2,
      payload: {
        request_id: "stream-b",
        conversation_id: "thread-a",
        revision: 9,
        conversation: {
          available: true,
          session_id: "brain-thread:thread-a",
          events: [
            { id: "history", seq: 1, kind: "user_message", body: "earlier" },
            { id: admissionId, seq: 2, kind: "user_message", body },
            {
              id: "assistant-1",
              seq: 3,
              kind: "assistant_message",
              body: "assistant reply after cwd vanished",
            },
          ],
        },
      },
    });
    expect(afterRestart.pendingUserMessages).toEqual([]);
    expect(afterRestart.conversation?.events.map((event) => event.id)).toEqual([
      "history",
      admissionId,
      "assistant-1",
    ]);

    // Later provider echo of the same admitted body must not reopen Pending.
    const echoed = interfaceChatThreadReducer(afterRestart, {
      type: "snapshot",
      generation: 2,
      payload: {
        request_id: "stream-b",
        conversation_id: "thread-a",
        revision: 10,
        conversation: {
          available: true,
          session_id: "brain-thread:thread-a",
          events: [
            { id: "history", seq: 1, kind: "user_message", body: "earlier" },
            { id: admissionId, seq: 2, kind: "user_message", body },
            {
              id: "assistant-1",
              seq: 3,
              kind: "assistant_message",
              body: "assistant reply after cwd vanished",
            },
          ],
        },
      },
    });
    expect(echoed.pendingUserMessages).toEqual([]);
  });

  test("exact receipt match clears Pending even when admission seq is inside the send boundary", () => {
    // Live duplicate proof: id=msh1e2ak_atzbs1 arrived while FIFO seq bounds
    // still blocked causal consumption of that same event.
    const receiptId = "msh1e2ak_atzbs1";
    const body = "duplicate pending plus durable admission";
    const withPending = interfaceChatThreadReducer(streamingState(), {
      type: "add_pending_user_message",
      message: pending({
        id: "local-optimistic",
        body,
        sentText: body,
        dispatchRequestId: receiptId,
        createdAfterMaxSeq: 99,
        createdAfterEventIds: ["history"],
      }),
    });
    expect(withPending.pendingUserMessages).toHaveLength(1);

    const admitted = interfaceChatThreadReducer(withPending, {
      type: "snapshot",
      generation: 1,
      payload: {
        request_id: "stream-a",
        conversation_id: "thread-a",
        revision: 11,
        conversation: {
          available: true,
          session_id: "brain-thread:thread-a",
          events: [
            { id: "history", seq: 1, kind: "user_message", body: "earlier" },
            { id: receiptId, seq: 5, kind: "user_message", body },
          ],
        },
      },
    });
    expect(admitted.pendingUserMessages).toEqual([]);
    expect(
      admitted.conversation?.events.filter(
        (event) => event.kind === "user_message" && event.body === body,
      ),
    ).toHaveLength(1);
    expect(admitted.turnFocusAnchorAliases.get(receiptId)).toBe(
      "local-optimistic",
    );
  });
});
