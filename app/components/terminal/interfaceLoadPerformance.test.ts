import { describe, expect, mock, test } from "bun:test";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import type {
  CodexConversationDeltaPayload,
  CodexConversationSnapshotPayload,
} from "../../services/websocket";
import {
  reconcileConversationDeltaEvents,
  reconcileConversationSnapshot,
} from "./interfaceConversationReconciliation";
import { projectZenTimeline } from "./projectZenTimeline";
import { buildZenTimelineFromSortedEvents } from "./InterfaceTimelineModel";
import { normalizeCodexConversation } from "../../services/codexConversation";

mock.module("react-native", () => ({ Platform: { OS: "web" } }));
mock.module("../../services/auth", () => ({
  buildAuthorizationHeader: async () => "test-authorization",
}));
mock.module("../../services/connectionIssue", () => ({
  diagnoseConnectionIssue: async () => null,
}));

const { interfaceChatThreadReducer } = await import("./InterfaceChatSession");

/**
 * Deterministic App-side performance regression fixtures.
 * Synthetic, content-free bodies of representative size (200 turns, ~800
 * events, ~3.5MB of payload text). Timing is wall/CPU on the host JS engine
 * and is honest: samples are retained counts/durations only, never bodies.
 */

const SYNTHETIC_SESSION_ID = "ses_perf_large";
const SYNTHETIC_REVISIT_SESSION_ID = "ses_perf_revisit";

function syntheticEvents(): CodexConversationEvent[] {
  const turns = 200;
  const base = Date.parse("2026-08-06T00:00:00.000Z");
  const events: CodexConversationEvent[] = [];
  let seq = 0;
  for (let turn = 0; turn < turns; turn += 1) {
    seq += 1;
    events.push({
      id: `evt:user:${turn}`,
      seq,
      timestamp: new Date(base + (turn * 4 + 1) * 1000).toISOString(),
      kind: "user_message",
      role: "user",
      body: "u".repeat(2000),
    });
    seq += 1;
    events.push({
      id: `evt:reasoning:${turn}`,
      seq,
      timestamp: new Date(base + (turn * 4 + 2) * 1000).toISOString(),
      kind: "commentary",
      role: "assistant",
      body: "r".repeat(1000),
      transient: true,
    });
    seq += 1;
    events.push({
      id: `evt:text:${turn}`,
      seq,
      timestamp: new Date(base + (turn * 4 + 2) * 1000).toISOString(),
      kind: "assistant_message",
      role: "assistant",
      body: "a".repeat(4000),
    });
    seq += 1;
    events.push({
      id: `evt:tool:${turn}`,
      seq,
      timestamp: new Date(base + (turn * 4 + 3) * 1000).toISOString(),
      kind: "tool",
      tool_name: "shell",
      call_id: `call:${turn}`,
      input: `{"command":"ls"}`,
      output: "o".repeat(12000),
      status: "completed",
    });
  }
  return events;
}

function syntheticConversation(): CodexConversation {
  return {
    available: true,
    source: "opencode_db",
    path: "/repo/.local/share/opencode/opencode.db",
    session_id: SYNTHETIC_SESSION_ID,
    cwd: "/repo/perf",
    updated_at: new Date().toISOString(),
    events: syntheticEvents(),
  };
}

function timed<T>(label: string, run: () => T): T {
  const started = Bun.nanoseconds();
  const result = run();
  const durationMs = (Bun.nanoseconds() - started) / 1e6;
  console.log(
    `zen-interface-perf app ${label}: ${durationMs.toFixed(3)}ms`,
  );
  return result;
}

describe("app interface load performance regression", () => {
  test("initial snapshot path timings (content-free)", () => {
    const raw = syntheticConversation();

    const normalized = timed("normalize-snapshot", () =>
      normalizeCodexConversation(raw),
    );
    expect(normalized.events.length).toBeGreaterThan(700);

    const reconciled = timed("reconcile-snapshot", () =>
      reconcileConversationSnapshot(null, raw, false),
    );
    expect(reconciled.events.length).toBe(normalized.events.length);

    const projected = timed("project-timeline-full", () =>
      projectZenTimeline(reconciled.events, null),
    );
    expect(projected.items.length).toBeGreaterThan(700);

    timed("build-timeline-full", () =>
      buildZenTimelineFromSortedEvents(reconciled.events),
    );
  });

  test("incremental delta path timings (content-free)", () => {
    const snapshot = timed("snapshot-reduce", () => {
      let state = interfaceChatThreadReducer(
        {
          cacheKey: "perf",
          conversation: null,
          loading: true,
          error: null,
          pendingUserMessages: [],
          turnFocusAnchorAliases: new Map(),
          streamCursor: { revision: 0 },
          awaitingSnapshot: false,
          resyncToken: 0,
        },
        {
          type: "cache_key_changed",
          cacheKey: "perf",
        },
      );
      state = interfaceChatThreadReducer(state, {
        type: "stream_start",
        generation: 1,
      });
      const payload: CodexConversationSnapshotPayload = {
        request_id: "perf-sub",
        agent_id: "agent-1",
        conversation_id: SYNTHETIC_SESSION_ID,
        revision: 1,
        server_generation: "gen-1",
        conversation: syntheticConversation(),
      };
      return interfaceChatThreadReducer(state, {
        type: "snapshot",
        payload,
        generation: 1,
      });
    });
    expect(snapshot.conversation?.events.length).toBeGreaterThan(700);

    const upsert: CodexConversationEvent = {
      ...snapshot.conversation!.events[snapshot.conversation!.events.length - 1]!,
      output: "o".repeat(12000) + "x".repeat(64),
    };
    const delta: CodexConversationDeltaPayload = {
      request_id: "perf-sub",
      agent_id: "agent-1",
      conversation_id: SYNTHETIC_SESSION_ID,
      revision: 2,
      base_revision: 1,
      server_generation: "gen-1",
      available: true,
      source: "opencode_db",
      path: "/repo/.local/share/opencode/opencode.db",
      session_id: SYNTHETIC_SESSION_ID,
      cwd: "/repo/perf",
      upserts: [upsert],
      deletes: [],
    };

    const afterDelta = timed("delta-reduce-one-upsert", () =>
      interfaceChatThreadReducer(snapshot, {
        type: "delta",
        delta,
        generation: 1,
      }),
    );
    expect(afterDelta.conversation?.events.length).toBe(
      snapshot.conversation!.events.length,
    );

    timed("delta-reconcile-events", () =>
      reconcileConversationDeltaEvents(
        snapshot.conversation!.events,
        [upsert],
        [],
      ),
    );
  });

  test("revisit snapshot path reuses cleaning and projection (content-free)", () => {
    const first = timed("snapshot-reduce-revisit-cold", () => {
      const initial = {
        cacheKey: "perf",
        conversation: null,
        loading: true,
        error: null,
        pendingUserMessages: [],
        turnFocusAnchorAliases: new Map(),
        streamCursor: { revision: 0 },
        awaitingSnapshot: false,
        resyncToken: 0,
      };
      let state = interfaceChatThreadReducer(initial, {
        type: "stream_start",
        generation: 1,
      });
      const payload: CodexConversationSnapshotPayload = {
        request_id: "perf-sub-1",
        agent_id: "agent-1",
        conversation_id: SYNTHETIC_REVISIT_SESSION_ID,
        revision: 1,
        server_generation: "gen-1",
        conversation: { ...syntheticConversation(), session_id: SYNTHETIC_REVISIT_SESSION_ID },
      };
      return interfaceChatThreadReducer(state, {
        type: "snapshot",
        payload,
        generation: 1,
      });
    });
    expect(first.conversation?.events.length).toBeGreaterThan(700);

    // Revisit = fresh reducer state (screen remount) with the identical
    // provider history. The cleaned conversation cache must short-circuit the
    // full cleaning pass, and the projection cache must short-circuit the full
    // timeline projection.
    const second = timed("snapshot-reduce-revisit-warm", () => {
      const initial = {
        cacheKey: "perf",
        conversation: null,
        loading: true,
        error: null,
        pendingUserMessages: [],
        turnFocusAnchorAliases: new Map(),
        streamCursor: { revision: 0 },
        awaitingSnapshot: false,
        resyncToken: 0,
      };
      let state = interfaceChatThreadReducer(initial, {
        type: "stream_start",
        generation: 2,
      });
      const payload: CodexConversationSnapshotPayload = {
        request_id: "perf-sub-2",
        agent_id: "agent-1",
        conversation_id: SYNTHETIC_REVISIT_SESSION_ID,
        revision: 1,
        server_generation: "gen-2",
        conversation: { ...syntheticConversation(), session_id: SYNTHETIC_REVISIT_SESSION_ID },
      };
      return interfaceChatThreadReducer(state, {
        type: "snapshot",
        payload,
        generation: 2,
      });
    });
    expect(second.conversation?.events.length).toBeGreaterThan(700);

    const events = second.conversation!.events;
    const projected = timed("project-timeline-revisit-warm", () =>
      projectZenTimeline(events, null),
    );
    expect(projected.items.length).toBeGreaterThan(700);
  });

  test("timeline incremental path after one upsert (content-free)", () => {
    const events = syntheticEvents();
    const first = projectZenTimeline(events, null);
    const changed = events.slice();
    changed[changed.length - 1] = {
      ...changed[changed.length - 1]!,
      output: "o".repeat(12000) + "y".repeat(64),
    };
    const second = timed("project-timeline-incremental", () =>
      projectZenTimeline(changed, first.cache),
    );
    expect(second.mode).toBe("incremental");
  });
});
