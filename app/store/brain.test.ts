import { describe, expect, test } from "bun:test";
import {
  brainReducer,
  brainThreadKey,
  initialBrainState,
  totalBrainUnread,
} from "./brain";

const snapshot = {
  chat_thread_id: "thread-1",
  scheduled_results: [
    {
      id: "calendar_result:item-1:run-1",
      thread_id: "thread-1",
      role: "assistant",
      body: "Daily papers completed",
      created_at: "2026-07-14T01:02:00Z",
      kind: "calendar_result",
      status: "completed",
      title: "Daily papers",
    },
  ],
};

describe("brain scheduled result unread state", () => {
  test("observes canonical results and marks a thread read", () => {
    const received = brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: "server-1",
      serverName: "Zen",
      serverUrl: "ws://zen",
      brain: snapshot,
    });
    expect(received.unreadByThread[brainThreadKey("server-1", "thread-1")]).toBe(1);
    expect(totalBrainUnread(received)).toBe(1);

    const read = brainReducer(received, {
      type: "BRAIN_THREAD_READ",
      serverId: "server-1",
      threadId: "thread-1",
    });
    expect(totalBrainUnread(read)).toBe(0);
    expect(read.readCursors[brainThreadKey("server-1", "thread-1")]).toContain(
      "calendar_result:item-1:run-1",
    );
  });

  test("hydrated device cursor prevents already-read results becoming unread", () => {
    const received = brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: "server-1",
      serverName: "Zen",
      serverUrl: "ws://zen",
      brain: snapshot,
    });
    const hydrated = brainReducer(received, {
      type: "BRAIN_READ_CURSORS_HYDRATED",
      cursors: {
        [brainThreadKey("server-1", "thread-1")]:
          "2026-07-14T01:02:00Z\u0000calendar_result:item-1:run-1",
      },
    });
    expect(hydrated.cursorsHydrated).toBe(true);
    expect(totalBrainUnread(hydrated)).toBe(0);
  });

  test("normalizes out-of-order and duplicate result snapshots before cursoring", () => {
    const received = brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: "server-1",
      serverName: "Zen",
      serverUrl: "ws://zen",
      brain: {
        ...snapshot,
        scheduled_results: [
          { ...snapshot.scheduled_results[0], id: "later", created_at: "2026-07-14T01:03:00Z" },
          { ...snapshot.scheduled_results[0], id: "earlier", created_at: "2026-07-14T01:01:00Z" },
          { ...snapshot.scheduled_results[0], id: "later", created_at: "2026-07-14T01:03:00Z" },
        ],
      },
    });

    expect(received.byServer["server-1"]?.scheduled_results?.map((item) => item.id)).toEqual([
      "earlier",
      "later",
    ]);
    expect(received.unreadByThread[brainThreadKey("server-1", "thread-1")]).toBe(2);
  });

  test("counts an equal-time result after an evicted cursor by deterministic id", () => {
    const received = brainReducer(
      {
        ...initialBrainState,
        readCursors: {
          [brainThreadKey("server-1", "thread-1")]:
            "2026-07-14T01:02:00Z\u0000calendar-a",
        },
      },
      {
        type: "BRAIN_SNAPSHOT",
        serverId: "server-1",
        serverName: "Zen",
        serverUrl: "ws://zen",
        brain: {
          ...snapshot,
          scheduled_results: [
            {
              ...snapshot.scheduled_results[0],
              id: "calendar-b",
              created_at: "2026-07-14T01:02:00Z",
            },
          ],
        },
      },
    );

    expect(received.unreadByThread[brainThreadKey("server-1", "thread-1")]).toBe(1);
  });
});
