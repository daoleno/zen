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
            title: "Release Zen",
            status: "running",
            owner_session_id: "agent-a",
            unread_result: false,
            claim_token: "must-not-project",
          },
          {
            work_id: "work-c",
            title: "Review sources",
            status: "waiting",
            wait_for: "Calendar occurrence",
            unread_result: true,
          },
          {
            work_id: "invalid",
            title: "Invalid",
            status: "workflow_paused",
            unread_result: true,
          },
        ],
      },
    });

    expect(received.byServer["server-1"]?.active_work).toEqual([
      {
        work_id: "work-a",
        title: "Release Zen",
        status: "running",
        owner_session_id: "agent-a",
        wait_for: undefined,
        unread_result: false,
      },
      {
        work_id: "work-c",
        title: "Review sources",
        status: "waiting",
        owner_session_id: undefined,
        wait_for: "Calendar occurrence",
        unread_result: true,
      },
    ]);
  });
});

describe("Brain Work result-event normalization", () => {
  test("keeps only known result kinds and deduplicates chronologically by event identity", () => {
    const base = {
      event_id: "event-b",
      kind: "session.done",
      work_id: "work-a",
      work_title: "Ship Brain cards",
      summary: "Focused implementation completed.",
      session_id: "brain-agent-cards:@1",
      session_name: "Brain cards",
      occurred_at: "2026-08-04T01:02:00Z",
      unread: true,
      claimed_at: "must-not-project",
      delivery_host_session_id: "must-not-project",
    };
    const received = brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: "server-1",
      serverName: "Zen",
      serverUrl: "ws://zen",
      brain: {
        result_events: [
          base,
          {
            ...base,
            event_id: "event-a",
            kind: "session.stale",
            occurred_at: "2026-08-04T01:01:00Z",
            session_id: "",
            session_name: "",
          },
          { ...base, summary: "Latest snapshot value.", unread: false },
          { ...base, event_id: "calendar", kind: "calendar.failure" },
          { ...base, event_id: "wake", kind: "scheduler.wake" },
          { ...base, event_id: "malformed", occurred_at: "not-a-date" },
        ],
      },
    });

    expect(received.byServer["server-1"]?.result_events).toEqual([
      {
        event_id: "event-a",
        kind: "session.stale",
        work_id: "work-a",
        work_title: "Ship Brain cards",
        summary: "Focused implementation completed.",
        session_id: undefined,
        session_name: undefined,
        occurred_at: "2026-08-04T01:01:00Z",
        unread: true,
      },
      {
        event_id: "event-b",
        kind: "session.done",
        work_id: "work-a",
        work_title: "Ship Brain cards",
        summary: "Latest snapshot value.",
        session_id: "brain-agent-cards:@1",
        session_name: "Brain cards",
        occurred_at: "2026-08-04T01:02:00Z",
        unread: false,
      },
    ]);
  });

  test("reconnect snapshot replaces result_events so server-omitted historical cards cannot resurrect", () => {
    const historical = {
      event_id: "1a6ddd99-accept",
      kind: "session.done",
      work_id: "work-historical",
      work_title: "zen-pi-opencode-first-class-acceptance",
      summary: "Round-2 ACCEPT P0=0 P1=0 P2=0",
      occurred_at: "2026-08-05T20:54:04Z",
      unread: true,
    };
    const current = {
      event_id: "current-needs-input",
      kind: "session.needs_input",
      work_id: "work-current",
      work_title: "zen-manual-input-and-brand-icons",
      summary: "go vet ./... ; echo VET_EXIT:$?",
      occurred_at: "2026-08-06T02:19:33Z",
      unread: true,
    };
    const before = brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: "server-1",
      serverName: "Zen",
      serverUrl: "ws://zen",
      brain: { result_events: [historical, current] },
    });
    expect(before.byServer["server-1"]?.result_events?.map((event) => event.event_id)).toEqual([
      "1a6ddd99-accept",
      "current-needs-input",
    ]);

    // Authoritative server projection already omitted the closed-commitment card.
    const afterReconnect = brainReducer(before, {
      type: "BRAIN_SNAPSHOT",
      serverId: "server-1",
      serverName: "Zen",
      serverUrl: "ws://zen",
      brain: { result_events: [current] },
    });
    expect(afterReconnect.byServer["server-1"]?.result_events).toEqual([
      {
        event_id: "current-needs-input",
        kind: "session.needs_input",
        work_id: "work-current",
        work_title: "zen-manual-input-and-brand-icons",
        summary: "go vet ./... ; echo VET_EXIT:$?",
        session_id: undefined,
        session_name: undefined,
        occurred_at: "2026-08-06T02:19:33Z",
        unread: true,
      },
    ]);
  });
});
