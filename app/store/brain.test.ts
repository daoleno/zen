import { describe, expect, test } from "bun:test";
import {
  brainReducer,
  initialBrainState,
} from "./brain";

const snapshot = {
  chat_thread_id: "thread-1",
  scheduled_results: [
    {
      id: "calendar_result:item-1:run-1",
      thread_id: "thread-1",
      body: "Daily papers completed",
      created_at: "2026-07-14T01:02:00Z",
      status: "completed",
      title: "Daily papers",
      calendar_item_id: "item-1",
      calendar_run_id: "run-1",
      scheduled_for: "2026-07-14T01:00:00Z",
    },
  ],
};

describe("brain scheduled result normalization", () => {
  test("normalizes out-of-order and duplicate result snapshots", () => {
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
  });

  test("accepts only the dedicated scheduled-result shape", () => {
    const received = brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: "server-1",
      serverName: "Zen",
      serverUrl: "ws://zen",
      brain: {
        ...snapshot,
        scheduled_results: [
          {
            ...snapshot.scheduled_results[0],
            role: "assistant",
            kind: "calendar_result",
          },
          {
            ...snapshot.scheduled_results[0],
            id: "incomplete",
            calendar_run_id: undefined,
          },
        ],
      },
    });

    expect(received.byServer["server-1"]?.scheduled_results).toEqual([
      snapshot.scheduled_results[0],
    ]);
  });
});
