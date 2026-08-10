import { describe, expect, test } from "bun:test";
import type { BrainActiveWork } from "../../store/brain";
import type { SessionResourceSnapshot } from "../../services/sessionResourceSnapshot";
import {
  buildWorkResourcePresentation,
  buildWorkSignalObservatoryModel,
  workResourceRequestIdentity,
  type WorkSignalOwner,
} from "./workSignalObservatoryModel";

const owner: WorkSignalOwner = {
  sessionId: "main:@7",
  label: "Release review",
  status: "running",
  delegated: true,
};

describe("Work signal observatory model", () => {
  test("maps canonical owners, typed waits, attention, and outcomes without exposing refs", () => {
    const model = buildWorkSignalObservatoryModel(
      [
        work({
          work_id: "owned",
          title: "Ship Zen",
          progress_mode: "owned",
          owner_session_id: owner.sessionId,
          owner_delegated: true,
        }),
        work({
          work_id: "session-wait",
          title: "Review result",
          status: "waiting",
          progress_mode: "waiting",
          wake: {
            kind: "session_terminal",
            ref: "session:main:@7:turn:provider-4",
          },
          wait_for: "Review evidence",
        }),
        work({
          work_id: "calendar-wait",
          title: "Publish report",
          status: "waiting",
          progress_mode: "waiting",
          wake: {
            kind: "calendar_result",
            ref: "calendar:item-3:run-9",
          },
        }),
        work({
          work_id: "ready",
          title: "Resolve feedback",
          progress_mode: "ready",
          attention_pending: true,
        }),
        work({
          work_id: "done",
          title: "Prepare fixtures",
          status: "done",
          progress_mode: undefined,
          unread_result: true,
        }),
      ],
      [owner],
    );

    expect(model.items.map((item) => item.signalLabel)).toEqual([
      "Release review",
      "Waiting for Release review",
      "Waiting for Calendar",
      "Ready to continue",
      "Completed",
    ]);
    expect(model.items.map((item) => item.stage)).toEqual([
      "owned",
      "waiting",
      "waiting",
      "ready",
      "completed",
    ]);
    expect(model.items[1]?.targetSessionId).toBe(owner.sessionId);
    expect(model.items[1]?.accessibilityLabel).not.toContain("provider-4");
    expect(model.items[2]?.accessibilityLabel).not.toContain("item-3");
    expect(model.activeCount).toBe(4);
    expect(model.ownerCount).toBe(1);
    expect(model.waitingCount).toBe(2);
    expect(model.attentionCount).toBe(1);
    expect(model.outcomeCount).toBe(1);
    expect(model.allProgressAccountedFor).toBe(true);
  });

  test("keeps the ready-wait-ready-own-terminal lifecycle truthful and keyed by revision", () => {
    const lifecycle = [
      work({
        revision: 1,
        progress_mode: "ready",
        attention_pending: true,
      }),
      work({
        revision: 2,
        status: "waiting",
        progress_mode: "waiting",
        wake: { kind: "user_input", ref: "brain-thread:thread-1" },
      }),
      work({
        revision: 3,
        status: "needs_input",
        progress_mode: "ready",
        attention_pending: true,
      }),
      work({
        revision: 4,
        status: "running",
        progress_mode: "owned",
        owner_session_id: owner.sessionId,
        owner_delegated: true,
      }),
      work({
        revision: 5,
        status: "done",
        progress_mode: undefined,
        session_finalizations: [
          {
            session_id: owner.sessionId,
            delegated: true,
            state: "pending",
            updated_at: "2026-08-10T02:00:00Z",
          },
        ],
        unread_result: true,
      }),
      work({
        revision: 6,
        status: "done",
        progress_mode: undefined,
        session_finalizations: [
          {
            session_id: owner.sessionId,
            delegated: true,
            state: "failed",
            attempts: 1,
            last_error: "teardown failed",
            updated_at: "2026-08-10T02:00:01Z",
          },
        ],
        attention_pending: true,
        unread_result: true,
      }),
      work({
        revision: 7,
        status: "done",
        progress_mode: undefined,
        session_finalizations: [
          {
            session_id: owner.sessionId,
            delegated: true,
            state: "complete",
            attempts: 1,
            updated_at: "2026-08-10T02:00:02Z",
          },
        ],
        unread_result: true,
      }),
    ].map((state) => buildWorkSignalObservatoryModel([state], [owner]).items[0]!);

    expect(
      lifecycle.map((item) => [item.stage, item.signalLabel, item.tone]),
    ).toEqual([
      ["ready", "Ready to continue", "attention"],
      ["waiting", "Waiting for you", "waiting"],
      ["ready", "Needs your input", "attention"],
      ["owned", "Release review", "active"],
      ["completed", "Wrapping up", "attention"],
      ["completed", "Couldn’t finish cleanly", "failed"],
      ["completed", "Completed", "complete"],
    ]);
    expect(new Set(lifecycle.map((item) => item.transitionKey)).size).toBe(
      lifecycle.length,
    );
  });

  test("exposes impossible ownership, waiting, and attention shapes", () => {
    const model = buildWorkSignalObservatoryModel(
      [
        work({
          work_id: "ownerless",
          progress_mode: "owned",
        }),
        work({
          work_id: "wakeless",
          status: "waiting",
          progress_mode: "waiting",
        }),
        work({
          work_id: "signalless",
          progress_mode: "ready",
          attention_pending: false,
        }),
      ],
      [],
    );

    expect(model.items.map((item) => item.signalLabel)).toEqual([
      "No Session assigned",
      "Waiting details unavailable",
      "Next step unavailable",
    ]);
    expect(model.items.every((item) => item.contradiction)).toBe(true);
    expect(model.failureCount).toBe(3);
    expect(model.allProgressAccountedFor).toBe(false);
    expect(model.items.map((item) => item.accessibilityLabel).join(" ")).not.toMatch(
      /\b(?:wake|signal|brain|fact|attention|disposition)\b/i,
    );
  });

  test("keeps dozens of Work items deterministic and individually keyed", () => {
    const items = Array.from({ length: 64 }, (_, index) =>
      work({
        work_id: `work-${index}`,
        revision: index,
        title: `Work ${index}`,
        status: "waiting",
        progress_mode: "waiting",
        wake: { kind: "user_input", ref: `thread-${index}` },
      }),
    );
    const first = buildWorkSignalObservatoryModel(items, []);
    const second = buildWorkSignalObservatoryModel(items, []);

    expect(first.items).toHaveLength(64);
    expect(new Set(first.items.map((item) => item.id)).size).toBe(64);
    expect(first.items.map((item) => item.transitionKey)).toEqual(
      second.items.map((item) => item.transitionKey),
    );
  });
});

describe("Work resource pressure presentation", () => {
  test("uses the existing Session pool threshold and host pressure without inventing availability", () => {
    const snapshot: SessionResourceSnapshot = {
      agent_id: owner.sessionId,
      session: { delegated: true, managed: true },
      pool: {
        memory_current_bytes: 9_000,
        memory_high_bytes: 10_000,
      },
      host: { pressure: "ok" },
    };

    expect(
      buildWorkResourcePresentation({
        activeCount: 1,
        ownerCount: 1,
        connected: true,
        loading: false,
        snapshot,
        failed: false,
      }),
    ).toEqual({ state: "steady", label: "Memory 90%", level: 0.9 });

    expect(
      buildWorkResourcePresentation({
        activeCount: 1,
        ownerCount: 1,
        connected: true,
        loading: true,
        snapshot,
        failed: false,
      }),
    ).toEqual({ state: "steady", label: "Memory 90%", level: 0.9 });

    expect(
      buildWorkResourcePresentation({
        activeCount: 1,
        ownerCount: 1,
        connected: true,
        loading: false,
        snapshot: {
          ...snapshot,
          host: { pressure: "pressure" },
        },
        failed: false,
      }),
    ).toEqual({ state: "pressure", label: "Memory 90%", level: 0.9 });

    expect(
      buildWorkResourcePresentation({
        activeCount: 1,
        ownerCount: 1,
        connected: false,
        loading: false,
        snapshot,
        failed: false,
      }),
    ).toEqual({ state: "unavailable", label: "Resources paused" });
  });

  test("keys reads only to the current server, owner Session, and connection", () => {
    const identity = workResourceRequestIdentity("server-a", owner.sessionId, true);

    expect(identity).toBe(`server-a\u0000${owner.sessionId}`);
    expect(workResourceRequestIdentity("server-a", owner.sessionId, true)).toBe(identity);
    expect(workResourceRequestIdentity("server-b", owner.sessionId, true)).not.toBe(identity);
    expect(workResourceRequestIdentity("server-a", owner.sessionId, false)).toBeNull();
    expect(workResourceRequestIdentity(null, owner.sessionId, true)).toBeNull();
  });
});

function work(overrides: Partial<BrainActiveWork> = {}): BrainActiveWork {
  return {
    work_id: "work-1",
    revision: 0,
    title: "Investigate release",
    status: "running",
    progress_mode: "ready",
    owner_session_id: undefined,
    owner_delegated: undefined,
    wait_for: undefined,
    wake: undefined,
    attention_pending: false,
    session_finalizations: undefined,
    unread_result: false,
    ...overrides,
  };
}
