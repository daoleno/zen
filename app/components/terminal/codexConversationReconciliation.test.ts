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

  test("rejects a delta gap and requests a fresh snapshot", () => {
    const current = {
      requestId: "stream-a",
      conversationId: "thread-a",
      revision: 4,
      generation: 7,
    };
    const gap = acceptConversationEnvelope(current, {
      requestId: "stream-a",
      conversationId: "thread-a",
      baseRevision: 5,
      revision: 6,
      generation: 7,
      kind: "delta",
    });

    expect(gap).toMatchObject({ accepted: false, gap: true });
    expect(gap.cursor).toBe(current);
  });

  test("rejects an envelope from an obsolete subscription generation", () => {
    const current = {
      requestId: "stream-new",
      conversationId: "thread-a",
      revision: 2,
      generation: 9,
    };
    const obsolete = acceptConversationEnvelope(current, {
      requestId: "stream-old",
      conversationId: "thread-a",
      revision: 99,
      generation: 8,
      kind: "snapshot",
    });

    expect(obsolete).toMatchObject({ accepted: false, obsolete: true });
    expect(obsolete.cursor).toBe(current);
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

  test("a revisioned same-thread snapshot is an exact replacement", () => {
    const first = event("message-1", 1, "complete");
    const second = event("message-2", 2, "newest");
    const reconciled = reconcileConversationSnapshot(
      conversation("thread-a", [first, second]),
      conversation("thread-a", [event("message-1", 1, "streamed update")]),
      true,
    );

    expect(reconciled.events.map((item) => item.id)).toEqual(["message-1"]);
    expect(reconciled.events[0]?.body).toBe("streamed update");
  });

  test("empty and null snapshots explicitly clear events current Activity and queue", () => {
    const previous: CodexConversation = {
      ...conversation("thread-a", [event("history", 1)]),
      activity: { id: "activity", status: "running", started_at: "2026-07-16T01:00:00Z" },
      queued_turns: [{ id: "queued", status: "queued", started_at: "2026-07-16T01:00:01Z" }],
    };
    const empty = reconcileConversationSnapshot(previous, {
      available: true,
      session_id: "thread-a",
      activity: undefined,
      queued_turns: [],
      events: [],
    }, true);
    const absent = reconcileConversationSnapshot(empty, null, true);

    expect(empty).toMatchObject({ events: [], queued_turns: [] });
    expect(empty.activity).toBeUndefined();
    expect(absent.events).toEqual([]);
    expect(absent.activity).toBeUndefined();
    expect(absent.queued_turns).toEqual([]);
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

  test("a formerly appended Calendar result returns to canonical time after reload", () => {
    const previous = conversation("thread-a", [
      { ...event("later-assistant", 2), timestamp: "2026-07-14T01:02:00Z" },
      { ...event("calendar-result", 3), timestamp: "2026-07-14T01:01:00Z", source: "calendar_result" },
    ]);
    const incoming = conversation("thread-a", [
      { ...event("calendar-result", 1), timestamp: "2026-07-14T01:01:00Z", source: "calendar_result" },
      { ...event("later-assistant", 2), timestamp: "2026-07-14T01:02:00Z" },
    ]);

    const reloaded = reconcileConversationSnapshot(previous, incoming, true);
    expect(reloaded.events.map((item) => item.id)).toEqual([
      "calendar-result",
      "later-assistant",
    ]);

    const reconnected = reconcileConversationDeltaEvents(
      [previous.events[0]],
      [incoming.events[0]],
    );
    expect(reconnected.map((item) => item.id)).toEqual([
      "calendar-result",
      "later-assistant",
    ]);
  });

  test("timestamp and identity deterministically order equal-time Calendar results", () => {
    const reconciled = reconcileConversationDeltaEvents([], [
      { ...event("calendar-b", 2), timestamp: "2026-07-14T01:01:00Z", source: "calendar_result" },
      { ...event("calendar-a", 2), timestamp: "2026-07-14T01:01:00Z", source: "calendar_result" },
    ]);

    expect(reconciled.map((item) => item.id)).toEqual(["calendar-a", "calendar-b"]);
  });
});
