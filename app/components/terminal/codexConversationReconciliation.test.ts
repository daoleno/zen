// @ts-nocheck
import { describe, expect, test } from "bun:test";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import {
  EMPTY_CONVERSATION_STREAM_CURSOR,
  acceptConversationEnvelope,
  reconcileConversationDeltaEvents,
  reconcileConversationSnapshot,
} from "./codexConversationReconciliation";

function event(id: string, seq: number, body = id): CodexConversationEvent {
  return {
    id,
    seq,
    kind: "assistant_message",
    role: "assistant",
    body,
  };
}

function conversation(
  sessionId: string,
  events: CodexConversationEvent[],
): CodexConversation {
  return {
    available: true,
    source: "codex_rollout",
    session_id: sessionId,
    events,
  };
}

describe("conversation stream reconciliation", () => {
  test("ignores stale revisions within one subscription", () => {
    const first = acceptConversationEnvelope(
      EMPTY_CONVERSATION_STREAM_CURSOR,
      { requestId: "stream-a", conversationId: "thread-a", revision: 4 },
    );
    const stale = acceptConversationEnvelope(first.cursor, {
      requestId: "stream-a",
      conversationId: "thread-a",
      revision: 3,
    });

    expect(first.accepted).toBe(true);
    expect(stale.accepted).toBe(false);
    expect(stale.cursor).toBe(first.cursor);
  });

  test("accepts a restarted subscription while retaining logical identity", () => {
    const previous = {
      requestId: "stream-a",
      conversationId: "thread-a",
      revision: 9,
    };
    const restarted = acceptConversationEnvelope(previous, {
      requestId: "stream-b",
      conversationId: "thread-a",
      revision: 1,
    });

    expect(restarted.accepted).toBe(true);
    expect(restarted.sameConversation).toBe(true);
  });

  test("a cached thread identity distinguishes a replacement on first snapshot", () => {
    const replacement = acceptConversationEnvelope(
      { conversationId: "thread-a", revision: 0 },
      { requestId: "stream-b", conversationId: "thread-b", revision: 1 },
    );

    expect(replacement.accepted).toBe(true);
    expect(replacement.sameConversation).toBe(false);
  });

  test("sync status without an identity cannot erase the current thread identity", () => {
    const current = {
      requestId: "stream-a",
      conversationId: "thread-a",
      revision: 9,
    };
    const syncing = acceptConversationEnvelope(current, {
      requestId: "stream-b",
      revision: 1,
    });
    const replacement = acceptConversationEnvelope(syncing.cursor, {
      requestId: "stream-b",
      conversationId: "thread-b",
      revision: 2,
    });

    expect(syncing.cursor.conversationId).toBe("thread-a");
    expect(replacement.sameConversation).toBe(false);
  });

  test("a shorter same-thread snapshot cannot clear or regress history", () => {
    const first = event("message-1", 1, "complete");
    const second = event("message-2", 2, "newest");
    const reconciled = reconcileConversationSnapshot(
      conversation("thread-a", [first, second]),
      conversation("thread-a", [event("message-1", 1, "streamed update")]),
      true,
    );

    expect(reconciled.events.map((item) => item.id)).toEqual([
      "message-1",
      "message-2",
    ]);
    expect(reconciled.events[0]?.body).toBe("streamed update");
  });

  test("an actual conversation replacement starts a new logical list", () => {
    const replacement = reconcileConversationSnapshot(
      conversation("thread-a", [event("old", 1)]),
      conversation("thread-b", [event("new", 1)]),
      false,
    );

    expect(replacement.events.map((item) => item.id)).toEqual(["new"]);
  });

  test("streaming updates reconcile in place and transient deletes stay monotonic", () => {
    const reconciled = reconcileConversationDeltaEvents(
      [event("assistant", 2, "par")],
      [event("assistant", 2, "partial response")],
    );

    expect(reconciled).toHaveLength(1);
    expect(reconciled[0]?.id).toBe("assistant");
    expect(reconciled[0]?.body).toBe("partial response");
  });
});
