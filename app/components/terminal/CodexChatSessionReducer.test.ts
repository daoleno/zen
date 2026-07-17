import { describe, expect, mock, test } from "bun:test";
import type { ProviderActivity } from "../../services/codexConversation";
import type { PendingUserMessage } from "./CodexChatSession";

mock.module("react-native", () => ({ Platform: { OS: "web" } }));
mock.module("../../services/auth", () => ({
  buildAuthorizationHeader: async () => "test-authorization",
}));
mock.module("../../services/connectionIssue", () => ({
  diagnoseConnectionIssue: async () => null,
}));

const { codexChatThreadReducer } = await import("./CodexChatSession");

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
    createdAfterMaxSeq: 3,
    createdAfterEventIds: ["history"],
    ...overrides,
  };
}

function state(
  pendingUserMessages: PendingUserMessage[],
): Parameters<typeof codexChatThreadReducer>[0] {
  return {
    cacheKey: "server-a:agent:agent-a",
    conversation: null,
    loading: false,
    error: null,
    pendingUserMessages,
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
    ...(status === "running"
      ? {}
      : { settled_at: "2026-07-17T10:00:02.000Z" }),
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
  test("retains every live local row without destructive count truncation", () => {
    const existing = Array.from({ length: 15 }, (_, index) =>
      pending({ id: `pending-${index}` })
    );
    const next = codexChatThreadReducer(state(existing), {
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
    const next = codexChatThreadReducer(current, {
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

  test("a reconnected stream clears local pending rows", () => {
    const next = codexChatThreadReducer(
      state([pending(), pending({ id: "pending-b" })]),
      { type: "stream_start", generation: 2 },
    );
    expect(next.pendingUserMessages).toEqual([]);
    expect(next.streamCursor).toEqual({
      conversationId: undefined,
      revision: 0,
      generation: 2,
    });
  });

  test("the first subscription does not erase a row sent before effects ran", () => {
    const current = {
      ...state([pending()]),
      streamCursor: { revision: 0 },
    };
    const next = codexChatThreadReducer(current, {
      type: "stream_start",
      generation: 1,
    });
    expect(next.pendingUserMessages).toBe(current.pendingUserMessages);
  });

  test("a key round trip starts clean instead of restoring an old provider projection", () => {
    const acceptedA = codexChatThreadReducer(streamingState(), {
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

    const keyB = codexChatThreadReducer(acceptedA, {
      type: "cache_key_changed",
      cacheKey: "server-a:agent:agent-b",
    });
    const returnedA = codexChatThreadReducer(keyB, {
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
    const accepted = codexChatThreadReducer(streamingState(), {
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

    const restarted = codexChatThreadReducer(accepted, {
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
    const current = state([
      pending({ dispatchRequestId: "request-current" }),
    ]);
    const stale = codexChatThreadReducer(current, {
      type: "reject_pending_user_message",
      id: "pending-a",
      requestId: "request-previous",
      code: "old_failure",
      message: "old attempt",
    });
    expect(stale).toBe(current);

    const failed = codexChatThreadReducer(current, {
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
      pending({ id: "first", lifecycle: "failed" }),
      pending({ id: "second", lifecycle: "failed" }),
    ];
    const next = codexChatThreadReducer(state(rows), {
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
    });
    expect(next.pendingUserMessages[1]).toBe(rows[1]);
  });

  test("snapshot replacement publishes running and terminal Activity exactly", () => {
    const running = codexChatThreadReducer(streamingState(), {
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

    const terminal = codexChatThreadReducer(running, {
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
    const absent = codexChatThreadReducer(base, {
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

    const terminal = codexChatThreadReducer(absent, {
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

    const cleared = codexChatThreadReducer(terminal, {
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
    const synced = codexChatThreadReducer(current, {
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

    const local = codexChatThreadReducer(streamingState(), {
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
});
