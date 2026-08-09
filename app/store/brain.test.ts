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

describe("Brain Active work normalization", () => {
  test("keeps multiple minimal Work projections and rejects scheduler internals", () => {
    const received = brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: "server-1",
      serverName: "Zen",
      serverUrl: "ws://zen",
      brain: {
        active_work: [
          {
            work_id: "work-a",
            revision: 7,
            title: "Release Zen",
            status: "running",
            progress_mode: "owned",
            owner_session_id: "agent-a",
            owner_delegated: true,
            attention_pending: false,
            unread_result: false,
            claim_token: "must-not-project",
          },
          {
            work_id: "work-c",
            title: "Review sources",
            status: "waiting",
            progress_mode: "waiting",
            wait_for: "Calendar occurrence",
            wake: { kind: "calendar_result", ref: "calendar:run-c" },
            attention_pending: false,
            unread_result: true,
          },
          {
            work_id: "invalid",
            title: "Invalid",
            status: "workflow_paused",
            unread_result: true,
          },
          {
            work_id: "invalid-mode",
            title: "Missing progress owner",
            status: "running",
            attention_pending: false,
            unread_result: false,
          },
          {
            work_id: "work-terminal",
            revision: 9,
            title: "Finalizing sessions",
            status: "done",
            attention_pending: true,
            session_finalizations: [
              {
                session_id: "agent-successor",
                delegated: true,
                state: "pending",
                updated_at: "2026-08-10T01:00:00Z",
              },
            ],
            unread_result: true,
          },
        ],
      },
    });

    expect(received.byServer["server-1"]?.active_work).toEqual([
      {
        work_id: "work-a",
        revision: 7,
        title: "Release Zen",
        status: "running",
        progress_mode: "owned",
        owner_session_id: "agent-a",
        owner_delegated: true,
        wait_for: undefined,
        wake: undefined,
        attention_pending: false,
        session_finalizations: undefined,
        unread_result: false,
      },
      {
        work_id: "work-c",
        revision: 0,
        title: "Review sources",
        status: "waiting",
        progress_mode: "waiting",
        owner_session_id: undefined,
        owner_delegated: undefined,
        wait_for: "Calendar occurrence",
        wake: { kind: "calendar_result", ref: "calendar:run-c" },
        attention_pending: false,
        session_finalizations: undefined,
        unread_result: true,
      },
      {
        work_id: "work-terminal",
        revision: 9,
        title: "Finalizing sessions",
        status: "done",
        progress_mode: undefined,
        owner_session_id: undefined,
        owner_delegated: undefined,
        wait_for: undefined,
        wake: undefined,
        attention_pending: true,
        session_finalizations: [
          {
            session_id: "agent-successor",
            delegated: true,
            state: "pending",
            attempts: undefined,
            last_error: undefined,
            updated_at: "2026-08-10T01:00:00Z",
          },
        ],
        unread_result: true,
      },
    ]);
  });
});
