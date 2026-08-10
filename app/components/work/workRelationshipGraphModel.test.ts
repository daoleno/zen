import { describe, expect, test } from "bun:test";
import {
  agentReducer,
  initialAgentState,
  isAgentSessionListFreshForConnection,
  type State as AgentState,
} from "../../store/agents";
import {
  buildWorkRelationshipGraphModel,
  buildWorkRelationshipGraphProjection,
  layoutWorkRelationshipGraph,
  resolveWorkGraphSelection,
  workGraphOpenSessionAccessibilityLabel,
  workRelationshipGraphVisibleText,
  type WorkGraphOwner,
  type WorkGraphWorkNode,
} from "./workRelationshipGraphModel";
import {
  GRAPH_OWNER_CORRECTION,
  GRAPH_OWNER_FAILED,
  GRAPH_OWNER_RUNNING,
  GRAPH_PRODUCTION_OWNERS,
  GRAPH_PRODUCTION_WORK,
  GRAPH_RAW_SESSION_WAKE_REF,
  graphWork,
} from "./workRelationshipGraphFixtures";
import { WORK_GRAPH_CONTROL_TOUCH_REGIONS } from "./workSignalObservatoryInteraction";

describe("Work relationship graph projection", () => {
  test("projects Brain to running Work to its delegated Session", () => {
    const model = buildWorkRelationshipGraphModel(
      [GRAPH_PRODUCTION_WORK[0]!],
      [GRAPH_OWNER_RUNNING],
    );
    const work = onlyWork(model);
    const owner = model.nodes.find(
      (node) => node.kind === "endpoint" && node.endpointKind === "agent",
    );

    expect(work.stateLabel).toBe("Running");
    expect(work.relationshipLabel).toBe("Release review owns this");
    expect(owner?.title).toBe("Release review");
    expect(model.edges.map((edge) => edge.kind)).toEqual([
      "delegation",
      "ownership",
    ]);
    expect(work.accessibilityLabel).toBe(
      "Prepare the mobile release candidate. Running. Release review owns this.",
    );
  });

  test("initial Agent labels include every owned, waiting, and blocked relationship", () => {
    const failedSessionWait = graphWork({
      work_id: "failed-session-wait",
      title: "Wait for export checks",
      status: "waiting",
      progress_mode: "waiting",
      wake: {
        kind: "session_terminal",
        ref: `session:${GRAPH_OWNER_FAILED.sessionId}:turn:provider-turn-10`,
      },
    });
    const model = buildWorkRelationshipGraphModel(
      [
        GRAPH_PRODUCTION_WORK.find((item) => item.work_id === "failed-owner")!,
        failedSessionWait,
        GRAPH_PRODUCTION_WORK.find(
          (item) => item.work_id === "failed-finalization",
        )!,
      ],
      [GRAPH_OWNER_FAILED],
    );
    const endpoint = model.nodes.find(
      (node) =>
        node.kind === "endpoint" &&
        node.sessionId === GRAPH_OWNER_FAILED.sessionId,
    );

    expect(endpoint?.accessibilityLabel).toBe(
      "Export checks. Session. Blocked. Owns 1 Work item · 1 Work item waiting here · 1 blocked item.",
    );
  });

  test("renders typed Session, user, and Calendar waits as dashed-path semantics", () => {
    const sessionWait = GRAPH_PRODUCTION_WORK.find(
      (item) => item.work_id === "typed-session-wait",
    )!;
    const userWait = GRAPH_PRODUCTION_WORK.find(
      (item) => item.work_id === "user-wait",
    )!;
    const calendarWait = GRAPH_PRODUCTION_WORK.find(
      (item) => item.work_id === "calendar-wait",
    )!;
    const model = buildWorkRelationshipGraphModel(
      [calendarWait, sessionWait, userWait],
      [GRAPH_OWNER_RUNNING],
    );
    const work = workNodes(model);

    expect(work.map((node) => [node.title, node.stateLabel])).toEqual([
      ["Publish the scheduled report", "Waiting"],
      ["Summarize delegated findings", "Waiting"],
      ["Choose the release note emphasis", "Waiting"],
    ]);
    expect(work.map((node) => node.relationshipLabel)).toEqual([
      "Waiting for Calendar",
      "Waiting for Release review",
      "Waiting for you",
    ]);
    expect(model.edges.filter((edge) => edge.kind === "wait")).toHaveLength(3);
    expect(
      model.nodes
        .filter((node) => node.kind === "endpoint")
        .map((node) => node.title),
    ).toEqual(["Release review", "Calendar", "You"]);
  });

  test("uses Review for attention and omits relationships without a present endpoint", () => {
    const absentOwnerReview = graphWork({
      work_id: "absent-owner-review",
      title: "Review an absent owner result",
      progress_mode: "owned",
      owner_session_id: "brain-agent-absent:@99",
      owner_delegated: true,
      attention_state: "reviewing",
    });
    const ids = new Set([
      "review-ready",
      "correction",
      "failed-owner",
      "ownerless-contradiction",
      "failed-finalization",
    ]);
    const model = buildWorkRelationshipGraphModel(
      [
        ...GRAPH_PRODUCTION_WORK.filter((item) => ids.has(item.work_id)),
        absentOwnerReview,
      ],
      GRAPH_PRODUCTION_OWNERS,
      { visibleWorkLimit: 6 },
    );
    const byTitle = new Map(workNodes(model).map((node) => [node.title, node]));

    expect(byTitle.get("Review the relationship wording")?.stateLabel).toBe(
      "Review",
    );
    expect(byTitle.get("Correct compact Android spacing")?.stateLabel).toBe(
      "Review",
    );
    expect(byTitle.get("Review an absent owner result")?.relationshipLabel).toBe(
      "Brain is reviewing",
    );
    expect(byTitle.get("Verify the iOS export")?.stateLabel).toBe("Blocked");
    expect(byTitle.get("Close delegated export checks")?.stateLabel).toBe(
      "Blocked",
    );
    const ownerless = byTitle.get("Resolve the missing owner");
    expect(ownerless).toBeUndefined();
    expect(
      model.edges.filter(
        (edge) => edge.from === "brain" && edge.kind === "review",
      ),
    ).toHaveLength(3);
    expect(
      model.nodes.some(
        (node) => node.kind === "endpoint" && node.title === "Unavailable",
      ),
    ).toBe(false);
    expect(
      new Set(workNodes(model).map((node) => node.stateLabel)),
    ).toEqual(new Set(["Review", "Blocked"]));
  });

  test("omits settled historical outcomes so they cannot swamp live Work", () => {
    const model = buildWorkRelationshipGraphModel(
      GRAPH_PRODUCTION_WORK,
      GRAPH_PRODUCTION_OWNERS,
    );

    expect(model.totalWorkCount).toBe(8);
    expect(
      model.nodes.some(
        (node) => node.kind === "work" && node.title === "Historical finished Work",
      ),
    ).toBe(false);
  });

  test("ranks attention deterministically and pages overflow through +N without a list", () => {
    const first = buildWorkRelationshipGraphModel(
      GRAPH_PRODUCTION_WORK,
      GRAPH_PRODUCTION_OWNERS,
      { page: 0 },
    );
    const second = buildWorkRelationshipGraphModel(
      GRAPH_PRODUCTION_WORK,
      GRAPH_PRODUCTION_OWNERS,
      { page: 1 },
    );
    const third = buildWorkRelationshipGraphModel(
      GRAPH_PRODUCTION_WORK,
      GRAPH_PRODUCTION_OWNERS,
      { page: 2 },
    );

    expect(first.pageCount).toBe(2);
    expect(first.hiddenWorkCount).toBe(4);
    expect(workNodes(first).map((node) => node.workId)).toEqual([
      "owned-running",
      "failed-owner",
      "correction",
      "review-ready",
    ]);
    expect(
      first.nodes.find((node) => node.kind === "aggregate")?.title,
    ).toBe("+4 more");
    expect(workNodes(second).map((node) => node.workId)).toEqual([
      "calendar-wait",
      "typed-session-wait",
      "user-wait",
      "failed-finalization",
    ]);
    expect(workNodes(third).map((node) => node.workId)).toEqual([
      "owned-running",
      "failed-owner",
      "correction",
      "review-ready",
    ]);
    expect(
      buildWorkRelationshipGraphModel(
        GRAPH_PRODUCTION_WORK,
        GRAPH_PRODUCTION_OWNERS,
        { page: 3 },
      ).page,
    ).toBe(1);
  });

  test("never places raw wake refs, revisions, provider turns, or Session IDs in visible text", () => {
    const pages = [0, 1, 2].map((page) =>
      buildWorkRelationshipGraphModel(
        GRAPH_PRODUCTION_WORK,
        GRAPH_PRODUCTION_OWNERS,
        { page },
      ),
    );
    const visible = pages.flatMap(workRelationshipGraphVisibleText).join(" ");

    expect(visible).not.toContain(GRAPH_RAW_SESSION_WAKE_REF);
    expect(visible).not.toContain(GRAPH_OWNER_RUNNING.sessionId);
    expect(visible).not.toContain(GRAPH_OWNER_CORRECTION.sessionId);
    expect(visible).not.toContain(GRAPH_OWNER_FAILED.sessionId);
    expect(visible).not.toMatch(
      /provider-turn|\brevision\b|\bFact\b|\bAttention\b|\bDisposition\b|wait_for/i,
    );
  });
});

describe("Work relationship graph freshness and layout", () => {
  test("waits for current-server hydration and a full Session list in the current connection generation", () => {
    const serverId = "server-a";
    const activeWork = [GRAPH_PRODUCTION_WORK[0]!];
    let state = agentReducer(initialAgentState, {
      type: "SET_SERVER_CONNECTION_STATE",
      serverId,
      connectionState: "connected",
    });
    state = replaceServerAgents(state, serverId, [rawAgent("old-owner")]);

    expect(
      buildWorkRelationshipGraphProjection({
        currentServerHydrated: false,
        hasCurrentServer: true,
        brainHydrated: true,
        agentListFresh: true,
        work: activeWork,
        owners: ownersFromState(state, serverId),
      }),
    ).toEqual({ state: "updating" });
    expect(
      buildWorkRelationshipGraphProjection({
        currentServerHydrated: true,
        hasCurrentServer: false,
        brainHydrated: false,
        agentListFresh: false,
        work: activeWork,
        owners: [],
      }),
    ).toEqual({ state: "unavailable" });

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

    state = agentReducer(state, {
      type: "UPSERT_AGENT",
      serverId,
      serverName: "Server A",
      serverUrl: "https://server-a.example",
      agent: rawAgent(GRAPH_OWNER_RUNNING.sessionId),
    });
    expect(isAgentSessionListFreshForConnection(state, serverId)).toBe(false);
    expect(
      readyProjection(state, serverId, activeWork).state,
    ).toBe("updating");

    state = replaceServerAgents(state, serverId, [
      rawAgent(GRAPH_OWNER_RUNNING.sessionId, "Release review"),
    ]);
    const projection = readyProjection(state, serverId, activeWork);
    expect(projection.state).toBe("ready");
    if (projection.state !== "ready") throw new Error("expected ready graph");
    expect(onlyWork(projection.model).relationshipLabel).toBe(
      "Release review owns this",
    );
  });

  test("keeps ordering and 360dp coordinates stable through timestamp-only updates and source reorders", () => {
    const work = GRAPH_PRODUCTION_WORK.filter(
      (item) => item.work_id !== "historical-finished",
    );
    const first = buildWorkRelationshipGraphModel(
      work,
      GRAPH_PRODUCTION_OWNERS,
      { page: 1 },
    );
    const timestampOnly = buildWorkRelationshipGraphModel(
      [...work]
        .reverse()
        .map((item) => ({ ...item, revision: item.revision + 100 })),
      [...GRAPH_PRODUCTION_OWNERS]
        .reverse()
        .map((owner) => ({ ...owner, updatedAt: (owner.updatedAt ?? 0) + 90_000 })),
      { page: 1 },
    );
    const firstLayout = layoutWorkRelationshipGraph(first, {
      width: 360,
      height: 328,
    });
    const nextLayout = layoutWorkRelationshipGraph(timestampOnly, {
      width: 360,
      height: 328,
    });

    expect(timestampOnly.visibleWorkNodeIds).toEqual(first.visibleWorkNodeIds);
    expect(
      nextLayout.nodes.map(({ node, x, y, width, height }) => ({
        id: node.id,
        x,
        y,
        width,
        height,
      })),
    ).toEqual(
      firstLayout.nodes.map(({ node, x, y, width, height }) => ({
        id: node.id,
        x,
        y,
        width,
        height,
      })),
    );
    expect(nextLayout.edges.map((edge) => edge.path)).toEqual(
      firstLayout.edges.map((edge) => edge.path),
    );
  });

  test("fits every node inside a 360 by 328 graph frame without middle-layer overlap", () => {
    const model = buildWorkRelationshipGraphModel(
      GRAPH_PRODUCTION_WORK,
      GRAPH_PRODUCTION_OWNERS,
      { page: 1 },
    );
    const layout = layoutWorkRelationshipGraph(model, {
      width: 360,
      height: 328,
    });
    const middle = layout.nodes
      .filter(({ node }) => node.kind === "work" || node.kind === "aggregate")
      .sort((left, right) => left.y - right.y);

    layout.nodes.forEach((node) => {
      expect(node.x).toBeGreaterThanOrEqual(0);
      expect(node.y).toBeGreaterThanOrEqual(0);
      expect(node.x + node.width).toBeLessThanOrEqual(360);
      expect(node.y + node.height).toBeLessThanOrEqual(328);
    });
    middle.slice(1).forEach((node, index) => {
      expect(node.y).toBeGreaterThanOrEqual(
        middle[index]!.y + middle[index]!.height,
      );
    });
    expect(
      middle.find(({ node }) => node.kind === "aggregate")?.height,
    ).toBe(WORK_GRAPH_CONTROL_TOUCH_REGIONS.aggregateHeight);
    expect(layout.edges).toHaveLength(model.edges.length);
  });
});

describe("Work relationship graph selection", () => {
  test("highlights only the selected Work path and exposes a compact Session action target", () => {
    const model = buildWorkRelationshipGraphModel(
      [GRAPH_PRODUCTION_WORK[0]!, GRAPH_PRODUCTION_WORK[1]!],
      [GRAPH_OWNER_RUNNING],
    );
    const running = workNodes(model).find((node) => node.state === "running")!;
    const selection = resolveWorkGraphSelection(model, running.id);

    expect(selection?.selectedNodeIds).toEqual([
      "brain",
      running.id,
      `endpoint:agent:${GRAPH_OWNER_RUNNING.sessionId}`,
    ]);
    expect(selection?.selectedEdgeIds).toHaveLength(2);
    expect(selection?.detail).toEqual({
      nodeId: running.id,
      title: "Prepare the mobile release candidate",
      state: "running",
      stateLabel: "Running",
      relationshipLabel: "Release review owns this",
      sessionTarget: {
        sessionId: GRAPH_OWNER_RUNNING.sessionId,
        title: "Release review",
      },
    });
    expect(
      workGraphOpenSessionAccessibilityLabel(selection!.detail.sessionTarget!),
    ).toBe("Open Release review Session");
    expect(
      workGraphOpenSessionAccessibilityLabel(selection!.detail.sessionTarget!),
    ).not.toContain("Prepare the mobile release candidate");
  });

  test("selecting an Agent highlights every owned or waiting Work path to Brain", () => {
    const model = buildWorkRelationshipGraphModel(
      [GRAPH_PRODUCTION_WORK[0]!, GRAPH_PRODUCTION_WORK[1]!],
      [GRAPH_OWNER_RUNNING],
    );
    const agentId = `endpoint:agent:${GRAPH_OWNER_RUNNING.sessionId}`;
    const selection = resolveWorkGraphSelection(model, agentId);

    expect(selection?.selectedNodeIds).toEqual([
      "brain",
      ...model.visibleWorkNodeIds,
      agentId,
    ]);
    expect(selection?.selectedEdgeIds).toHaveLength(4);
    expect(selection?.detail.relationshipLabel).toBe(
      "Owns 1 Work item · 1 Work item waiting here",
    );
    expect(selection?.detail.sessionTarget).toEqual({
      sessionId: GRAPH_OWNER_RUNNING.sessionId,
      title: "Release review",
    });
  });
});

function workNodes(model: ReturnType<typeof buildWorkRelationshipGraphModel>) {
  return model.nodes.filter(
    (node): node is WorkGraphWorkNode => node.kind === "work",
  );
}

function onlyWork(model: ReturnType<typeof buildWorkRelationshipGraphModel>) {
  const nodes = workNodes(model);
  if (nodes.length !== 1) throw new Error(`expected one Work node, got ${nodes.length}`);
  return nodes[0]!;
}

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

function rawAgent(id: string, name = id) {
  return {
    id,
    name,
    status: "running" as const,
    delegated: true,
    summary: "Working",
  };
}

function ownersFromState(
  state: AgentState,
  serverId: string,
): WorkGraphOwner[] {
  return state.agents
    .filter((agent) => agent.serverId === serverId)
    .map((agent) => ({
      sessionId: agent.id,
      label: agent.name,
      status: agent.status,
      delegated: agent.delegated === true,
      needsAttention: agent.needs_attention === true,
      updatedAt: agent.updated_at,
    }));
}

function readyProjection(
  state: AgentState,
  serverId: string,
  work: Parameters<typeof buildWorkRelationshipGraphProjection>[0]["work"],
) {
  return buildWorkRelationshipGraphProjection({
    currentServerHydrated: true,
    hasCurrentServer: true,
    brainHydrated: true,
    agentListFresh: isAgentSessionListFreshForConnection(state, serverId),
    work,
    owners: ownersFromState(state, serverId),
  });
}
