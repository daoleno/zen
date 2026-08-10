import { describe, expect, test } from "bun:test";
import type { BrainActiveWork } from "../../store/brain";
import {
  agentReducer,
  initialAgentState,
  isAgentSessionListFreshForConnection,
  type State as AgentState,
} from "../../store/agents";
import type { SessionResourceSnapshot } from "../../services/sessionResourceSnapshot";
import {
  buildWorkResourcePresentation,
  buildWorkSignalObservatoryModel,
  buildWorkSignalObservatoryProjection,
  projectWorkResourceRequest,
  reconcileStableWorkSignalItems,
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
  test("maps the daemon WaitFor=Wake.Ref fixture without exposing its canonical Session Turn ref", () => {
    const producerTurnId = `${owner.sessionId}:turn:1`;
    const daemonWaitRef = `session:${owner.sessionId}:turn:${producerTurnId}`;
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
            ref: daemonWaitRef,
          },
          // WorkDispositionWait stores this exact identity in both fields.
          wait_for: daemonWaitRef,
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
    expect(model.items[1]?.detail).toBeUndefined();
    expect(model.items[1]?.signalLabel).not.toContain(daemonWaitRef);
    expect(model.items[1]?.accessibilityLabel).not.toContain(daemonWaitRef);
    expect(model.items[1]?.accessibilityLabel).not.toContain("turn:1");
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

describe("Work observatory projection integration", () => {
  test("waits for Brain hydration and the current generation full Session list", () => {
    const serverId = "server-a";
    const nextOwnerId = "brain-agent-next:@2";
    const activeWork = [
      work({
        progress_mode: "owned",
        owner_session_id: nextOwnerId,
        owner_delegated: true,
      }),
    ];
    let state = agentReducer(initialAgentState, {
      type: "SET_SERVER_CONNECTION_STATE",
      serverId,
      connectionState: "connected",
    });
    state = replaceServerAgents(state, serverId, [rawAgent("old-owner")]);
    expect(isAgentSessionListFreshForConnection(state, serverId)).toBe(true);
    expect(
      buildWorkSignalObservatoryProjection({
        brainHydrated: false,
        agentListFresh: true,
        work: activeWork,
        owners: ownersFromState(state, serverId),
      }),
    ).toEqual({ state: "updating" });

    state = agentReducer(state, {
      type: "SET_SERVER_CONNECTION_STATE",
      serverId,
      connectionState: "offline",
    });
    state = agentReducer(state, {
      type: "SET_SERVER_CONNECTION_STATE",
      serverId,
      connectionState: "connected",
    });
    expect(isAgentSessionListFreshForConnection(state, serverId)).toBe(false);

    const beforeFullList = buildWorkSignalObservatoryProjection({
      brainHydrated: true,
      agentListFresh: isAgentSessionListFreshForConnection(state, serverId),
      work: activeWork,
      owners: ownersFromState(state, serverId),
    });
    expect(beforeFullList).toEqual({ state: "updating" });
    expect(JSON.stringify(beforeFullList)).not.toContain("Session unavailable");

    state = agentReducer(state, {
      type: "UPSERT_AGENT",
      serverId,
      serverName: "Server A",
      serverUrl: "https://server-a.example",
      agent: rawAgent(nextOwnerId),
    });
    expect(isAgentSessionListFreshForConnection(state, serverId)).toBe(false);
    expect(
      buildWorkSignalObservatoryProjection({
        brainHydrated: true,
        agentListFresh: isAgentSessionListFreshForConnection(state, serverId),
        work: activeWork,
        owners: ownersFromState(state, serverId),
      }),
    ).toEqual({ state: "updating" });

    state = replaceServerAgents(state, serverId, [rawAgent(nextOwnerId)]);
    const afterFullList = buildWorkSignalObservatoryProjection({
      brainHydrated: true,
      agentListFresh: isAgentSessionListFreshForConnection(state, serverId),
      work: activeWork,
      owners: ownersFromState(state, serverId),
    });
    expect(afterFullList.state).toBe("ready");
    if (afterFullList.state !== "ready") {
      throw new Error("expected a ready projection");
    }
    expect(afterFullList.model.items[0]?.signalLabel).toBe(nextOwnerId);
    expect(afterFullList.model.items[0]?.contradiction).toBe(false);
  });

  test("keeps mounted rows stable across UpdatedAt source reorders and appends newcomers", () => {
    const current = buildWorkSignalObservatoryModel(
      [
        work({ work_id: "a", title: "A", revision: 1 }),
        work({ work_id: "b", title: "B", revision: 1 }),
      ],
      [],
    ).items;
    const reordered = buildWorkSignalObservatoryModel(
      [
        work({ work_id: "b", title: "B updated", revision: 2 }),
        work({ work_id: "c", title: "C", revision: 1 }),
        work({ work_id: "a", title: "A updated", revision: 2 }),
      ],
      [],
    ).items;

    const stable = reconcileStableWorkSignalItems(current, reordered);
    expect(stable.map((item) => item.id)).toEqual(["a", "b", "c"]);
    expect(stable.map((item) => item.title)).toEqual([
      "A updated",
      "B updated",
      "C",
    ]);
    expect(reconcileStableWorkSignalItems(stable, reordered)).toBe(stable);

    const removed = reconcileStableWorkSignalItems(
      stable,
      reordered.filter((item) => item.id !== "a"),
    );
    expect(removed.map((item) => item.id)).toEqual(["b", "c"]);
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

  test("projects loading, success, and failure atomically by server, Session, and connection generation", () => {
    const identity = workResourceRequestIdentity(
      "server-a",
      owner.sessionId,
      true,
      4,
    );
    const nextConnectionIdentity = workResourceRequestIdentity(
      "server-a",
      owner.sessionId,
      true,
      5,
    );
    const nextServerIdentity = workResourceRequestIdentity(
      "server-b",
      owner.sessionId,
      true,
      1,
    );
    const nextOwnerIdentity = workResourceRequestIdentity(
      "server-a",
      "main:@8",
      true,
      4,
    );
    const snapshot: SessionResourceSnapshot = {
      agent_id: owner.sessionId,
      session: { delegated: true, managed: true },
    };
    if (
      !identity ||
      !nextConnectionIdentity ||
      !nextServerIdentity ||
      !nextOwnerIdentity
    ) {
      throw new Error("expected connected resource identities");
    }

    expect(identity).toBe(`server-a\u0000${owner.sessionId}\u00004`);
    expect(nextConnectionIdentity).not.toBe(identity);
    expect(
      workResourceRequestIdentity("server-b", owner.sessionId, true, 4),
    ).not.toBe(identity);
    expect(
      workResourceRequestIdentity("server-a", owner.sessionId, false, 4),
    ).toBeNull();
    expect(workResourceRequestIdentity(null, owner.sessionId, true, 4)).toBeNull();

    const ready = { identity, status: "ready" as const, snapshot };
    expect(projectWorkResourceRequest(identity, ready)).toEqual(ready);
    expect(projectWorkResourceRequest(nextConnectionIdentity, ready)).toEqual({
      identity: nextConnectionIdentity,
      status: "loading",
      snapshot: null,
    });
    expect(projectWorkResourceRequest(nextServerIdentity, ready)).toEqual({
      identity: nextServerIdentity,
      status: "loading",
      snapshot: null,
    });
    expect(projectWorkResourceRequest(nextOwnerIdentity, ready)).toEqual({
      identity: nextOwnerIdentity,
      status: "loading",
      snapshot: null,
    });
    expect(
      projectWorkResourceRequest(nextConnectionIdentity, {
        identity,
        status: "failed",
      }),
    ).toEqual({
      identity: nextConnectionIdentity,
      status: "loading",
      snapshot: null,
    });
    expect(projectWorkResourceRequest(null, ready)).toEqual({
      identity: null,
      status: "idle",
      snapshot: null,
    });
  });
});

function replaceServerAgents(
  state: AgentState,
  serverId: string,
  agents: ReturnType<typeof rawAgent>[],
): AgentState {
  return agentReducer(state, {
    type: "UPSERT_SERVER_AGENTS",
    serverId,
    serverName: "Server A",
    serverUrl: "https://server-a.example",
    agents,
  });
}

function rawAgent(id: string) {
  return {
    id,
    name: id,
    status: "running" as const,
    delegated: true,
    summary: "Working",
  };
}

function ownersFromState(
  state: AgentState,
  serverId: string,
): WorkSignalOwner[] {
  return state.agents
    .filter((agent) => agent.serverId === serverId)
    .map((agent) => ({
      sessionId: agent.id,
      label: agent.name,
      status: agent.status,
      delegated: agent.delegated === true,
    }));
}

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
