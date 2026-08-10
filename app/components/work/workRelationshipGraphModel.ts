import type { AgentStatus } from "../../constants/tokens";
import type { BrainActiveWork, BrainSessionFinalization } from "../../store/brain";

export const WORK_GRAPH_VISIBLE_WORK_LIMIT = 4;

export type WorkGraphState = "running" | "waiting" | "review" | "blocked";

export type WorkGraphOwner = {
  sessionId: string;
  label: string;
  status: AgentStatus;
  delegated: boolean;
  needsAttention?: boolean;
  /** Deliberately ignored by ranking and layout. */
  updatedAt?: number;
};

type WorkGraphNodeBase = {
  id: string;
  title: string;
  accessibilityLabel: string;
};

export type WorkGraphBrainNode = WorkGraphNodeBase & {
  kind: "brain";
};

export type WorkGraphWorkNode = WorkGraphNodeBase & {
  kind: "work";
  state: WorkGraphState;
  stateLabel: string;
  relationshipLabel: string;
  workId: string;
  endpointId?: string;
  sessionId?: string;
  contradiction: boolean;
};

export type WorkGraphEndpointNode = WorkGraphNodeBase & {
  kind: "endpoint";
  endpointKind: "agent" | "wake" | "placeholder";
  state: WorkGraphState;
  stateLabel: string;
  sessionId?: string;
};

export type WorkGraphAggregateNode = WorkGraphNodeBase & {
  kind: "aggregate";
  hiddenCount: number;
  page: number;
  pageCount: number;
};

export type WorkGraphNode =
  | WorkGraphBrainNode
  | WorkGraphWorkNode
  | WorkGraphEndpointNode
  | WorkGraphAggregateNode;

export type WorkGraphEdgeKind =
  | "delegation"
  | "ownership"
  | "wait"
  | "review"
  | "blocked";

export type WorkGraphEdge = {
  id: string;
  from: string;
  to: string;
  kind: WorkGraphEdgeKind;
};

export type WorkRelationshipGraphModel = {
  nodes: WorkGraphNode[];
  edges: WorkGraphEdge[];
  visibleWorkNodeIds: string[];
  totalWorkCount: number;
  hiddenWorkCount: number;
  page: number;
  pageCount: number;
  accessibilityLabel: string;
};

export type WorkRelationshipGraphProjection =
  | { state: "updating" }
  | { state: "unavailable" }
  | { state: "ready"; model: WorkRelationshipGraphModel };

export type WorkGraphViewport = {
  width: number;
  height: number;
};

export type WorkGraphNodeLayout = {
  node: WorkGraphNode;
  x: number;
  y: number;
  width: number;
  height: number;
  centerX: number;
  centerY: number;
};

export type WorkGraphEdgeLayout = WorkGraphEdge & {
  path: string;
  startX: number;
  startY: number;
  endX: number;
  endY: number;
};

export type WorkRelationshipGraphLayout = {
  viewport: WorkGraphViewport;
  nodes: WorkGraphNodeLayout[];
  edges: WorkGraphEdgeLayout[];
};

export type WorkGraphSelectionDetail = {
  nodeId: string;
  title: string;
  state: WorkGraphState;
  stateLabel: string;
  relationshipLabel: string;
  sessionId?: string;
};

export type WorkGraphSelection = {
  selectedNodeIds: string[];
  selectedEdgeIds: string[];
  detail: WorkGraphSelectionDetail;
};

type EndpointProjection = {
  key: string;
  title: string;
  kind: WorkGraphEndpointNode["endpointKind"];
  state: WorkGraphState;
  sessionId?: string;
};

type MeaningfulWork = {
  workId: string;
  title: string;
  terminal: boolean;
  state: WorkGraphState;
  relationshipLabel: string;
  endpoint?: EndpointProjection;
  endpointEdgeKind?: WorkGraphEdgeKind;
  sessionId?: string;
  contradiction: boolean;
};

const STATE_LABEL: Record<WorkGraphState, string> = {
  running: "Running",
  waiting: "Waiting",
  review: "Review",
  blocked: "Blocked",
};

const LIVE_STATE_RANK: Record<WorkGraphState, number> = {
  running: 0,
  blocked: 1,
  review: 2,
  waiting: 3,
};

const TERMINAL_STATE_RANK: Record<WorkGraphState, number> = {
  blocked: 4,
  running: 5,
  review: 6,
  waiting: 7,
};

export function buildWorkRelationshipGraphProjection({
  currentServerHydrated,
  hasCurrentServer,
  brainHydrated,
  agentListFresh,
  work,
  owners,
  page = 0,
  visibleWorkLimit = WORK_GRAPH_VISIBLE_WORK_LIMIT,
}: {
  currentServerHydrated: boolean;
  hasCurrentServer: boolean;
  brainHydrated: boolean;
  agentListFresh: boolean;
  work: readonly BrainActiveWork[];
  owners: readonly WorkGraphOwner[];
  page?: number;
  visibleWorkLimit?: number;
}): WorkRelationshipGraphProjection {
  if (!currentServerHydrated) {
    return { state: "updating" };
  }
  if (!hasCurrentServer) {
    return { state: "unavailable" };
  }
  if (!brainHydrated || !agentListFresh) {
    return { state: "updating" };
  }
  return {
    state: "ready",
    model: buildWorkRelationshipGraphModel(work, owners, {
      page,
      visibleWorkLimit,
    }),
  };
}

export function buildWorkRelationshipGraphModel(
  work: readonly BrainActiveWork[],
  owners: readonly WorkGraphOwner[],
  options: { page?: number; visibleWorkLimit?: number } = {},
): WorkRelationshipGraphModel {
  const ownerById = new Map(owners.map((owner) => [owner.sessionId, owner]));
  const meaningful = work
    .map((item) => projectMeaningfulWork(item, ownerById))
    .filter((item): item is MeaningfulWork => item !== null)
    .sort(compareMeaningfulWork);
  const visibleWorkLimit = normalizedVisibleLimit(options.visibleWorkLimit);
  const pageCount = Math.max(1, Math.ceil(meaningful.length / visibleWorkLimit));
  const page = normalizedPage(options.page ?? 0, pageCount);
  const visible = cyclicPage(meaningful, page, visibleWorkLimit);
  const hiddenWorkCount = meaningful.length - visible.length;
  const brainNode: WorkGraphBrainNode = {
    id: "brain",
    kind: "brain",
    title: "Brain",
    accessibilityLabel:
      meaningful.length === 0
        ? "Brain. No Work in progress."
        : `Brain. Coordinates ${countLabel(meaningful.length, "Work item")}.`,
  };
  const workNodes = visible.map(workNodeFromProjection);
  const endpoints = endpointNodes(visible);
  const aggregate =
    hiddenWorkCount > 0
      ? aggregateNode(hiddenWorkCount, page, pageCount)
      : null;
  const nodes: WorkGraphNode[] = [
    brainNode,
    ...workNodes,
    ...endpoints,
    ...(aggregate ? [aggregate] : []),
  ];
  const edges = workNodes.flatMap((node) => {
    const brainEdge: WorkGraphEdge = {
      id: `brain-to-${node.id}`,
      from: brainNode.id,
      to: node.id,
      kind:
        node.state === "review"
          ? "review"
          : node.state === "blocked"
            ? "blocked"
            : "delegation",
    };
    const endpointEdge = node.endpointId
      ? {
          id: `${node.id}-to-${node.endpointId}`,
          from: node.id,
          to: node.endpointId,
          kind:
            visible.find((item) => item.workId === node.workId)
              ?.endpointEdgeKind ?? "blocked",
        }
      : null;
    return endpointEdge ? [brainEdge, endpointEdge] : [brainEdge];
  });

  return {
    nodes,
    edges,
    visibleWorkNodeIds: workNodes.map((node) => node.id),
    totalWorkCount: meaningful.length,
    hiddenWorkCount,
    page,
    pageCount,
    accessibilityLabel:
      meaningful.length === 0
        ? "Work relationship graph. Nothing in progress."
        : `Work relationship graph. ${countLabel(meaningful.length, "meaningful Work item")}. ${stateSummary(meaningful)}.`,
  };
}

export function layoutWorkRelationshipGraph(
  model: WorkRelationshipGraphModel,
  viewport: WorkGraphViewport,
): WorkRelationshipGraphLayout {
  const width = finiteDimension(viewport.width);
  const height = finiteDimension(viewport.height);
  const safeViewport = { width, height };
  if (width === 0 || height === 0) {
    return { viewport: safeViewport, nodes: [], edges: [] };
  }

  const horizontalPadding = Math.max(8, Math.min(12, width * 0.028));
  const brainWidth = Math.max(58, Math.min(68, width * 0.19));
  const endpointWidth = Math.max(104, Math.min(112, width * 0.3));
  const workX = Math.max(brainWidth + horizontalPadding * 2, width * 0.255);
  const endpointX = width - horizontalPadding - endpointWidth;
  const workWidth = Math.max(104, endpointX - workX - 22);
  const workNodes = model.nodes.filter(
    (node): node is WorkGraphWorkNode => node.kind === "work",
  );
  const endpoints = model.nodes.filter(
    (node): node is WorkGraphEndpointNode => node.kind === "endpoint",
  );
  const aggregate = model.nodes.find(
    (node): node is WorkGraphAggregateNode => node.kind === "aggregate",
  );
  const middleNodes: Array<WorkGraphWorkNode | WorkGraphAggregateNode> = [
    ...workNodes,
    ...(aggregate ? [aggregate] : []),
  ];
  const slotCount = Math.max(middleNodes.length, endpoints.length, 1);
  const slotCenters = verticalSlotCenters(height, slotCount, 62);
  const middlePosition = new Map<string, WorkGraphNodeLayout>();
  middleNodes.forEach((node, index) => {
    const nodeHeight = node.kind === "aggregate" ? 42 : 62;
    middlePosition.set(
      node.id,
      nodeLayout(
        node,
        workX,
        slotCenters[index] - nodeHeight / 2,
        workWidth,
        nodeHeight,
      ),
    );
  });
  const endpointPosition = new Map<string, WorkGraphNodeLayout>();
  const endpointCenters = verticalSlotCenters(height, endpoints.length, 60);
  endpoints.forEach((node, index) => {
    endpointPosition.set(
      node.id,
      nodeLayout(
        node,
        endpointX,
        endpointCenters[index] - 28,
        endpointWidth,
        56,
      ),
    );
  });
  const brain = model.nodes.find(
    (node): node is WorkGraphBrainNode => node.kind === "brain",
  );
  const brainPosition = brain
    ? nodeLayout(
        brain,
        horizontalPadding,
        height / 2 - 26,
        brainWidth,
        52,
      )
    : null;
  const positionedById = new Map<string, WorkGraphNodeLayout>();
  if (brainPosition) positionedById.set(brainPosition.node.id, brainPosition);
  middlePosition.forEach((value, key) => positionedById.set(key, value));
  endpointPosition.forEach((value, key) => positionedById.set(key, value));
  const nodes = model.nodes
    .map((node) => positionedById.get(node.id))
    .filter((node): node is WorkGraphNodeLayout => Boolean(node));
  const edges = model.edges
    .map((edge) => layoutEdge(edge, positionedById))
    .filter((edge): edge is WorkGraphEdgeLayout => Boolean(edge));

  return { viewport: safeViewport, nodes, edges };
}

export function resolveWorkGraphSelection(
  model: WorkRelationshipGraphModel,
  nodeId: string | null,
): WorkGraphSelection | null {
  if (!nodeId) return null;
  const node = model.nodes.find((candidate) => candidate.id === nodeId);
  if (!node || node.kind === "brain" || node.kind === "aggregate") return null;

  const selectedEdgeIds = new Set<string>();
  const selectedNodeIds = new Set<string>([node.id, "brain"]);
  if (node.kind === "work") {
    model.edges.forEach((edge) => {
      if (edge.from === node.id || edge.to === node.id) {
        selectedEdgeIds.add(edge.id);
        selectedNodeIds.add(edge.from);
        selectedNodeIds.add(edge.to);
      }
    });
  } else {
    const connectedWorkIds = new Set<string>();
    model.edges.forEach((edge) => {
      if (edge.from === node.id || edge.to === node.id) {
        selectedEdgeIds.add(edge.id);
        selectedNodeIds.add(edge.from);
        selectedNodeIds.add(edge.to);
        const other = edge.from === node.id ? edge.to : edge.from;
        if (other.startsWith("work:")) connectedWorkIds.add(other);
      }
    });
    model.edges.forEach((edge) => {
      if (edge.from === "brain" && connectedWorkIds.has(edge.to)) {
        selectedEdgeIds.add(edge.id);
        selectedNodeIds.add(edge.from);
        selectedNodeIds.add(edge.to);
      }
    });
  }

  return {
    selectedNodeIds: model.nodes
      .filter((candidate) => selectedNodeIds.has(candidate.id))
      .map((candidate) => candidate.id),
    selectedEdgeIds: model.edges
      .filter((edge) => selectedEdgeIds.has(edge.id))
      .map((edge) => edge.id),
    detail:
      node.kind === "work"
        ? {
            nodeId: node.id,
            title: node.title,
            state: node.state,
            stateLabel: node.stateLabel,
            relationshipLabel: node.relationshipLabel,
            sessionId: node.sessionId,
          }
        : endpointSelectionDetail(node, model.edges),
  };
}

/** Returns only strings that a person can see or hear, never action identities. */
export function workRelationshipGraphVisibleText(
  model: WorkRelationshipGraphModel,
): string[] {
  const nodeText = model.nodes.flatMap((node) => {
    if (node.kind === "work") {
      return [
        node.title,
        node.stateLabel,
        node.relationshipLabel,
        node.accessibilityLabel,
      ];
    }
    if (node.kind === "endpoint") {
      return [node.title, node.stateLabel, node.accessibilityLabel];
    }
    return [node.title, node.accessibilityLabel];
  });
  const detailText = model.nodes.flatMap((node) => {
    const selection = resolveWorkGraphSelection(model, node.id);
    return selection
      ? [
          selection.detail.title,
          selection.detail.stateLabel,
          selection.detail.relationshipLabel,
        ]
      : [];
  });
  return [model.accessibilityLabel, ...nodeText, ...detailText];
}

function projectMeaningfulWork(
  work: BrainActiveWork,
  ownerById: ReadonlyMap<string, WorkGraphOwner>,
): MeaningfulWork | null {
  if (work.status === "done" || work.status === "cancelled") {
    return projectTerminalWork(work, ownerById);
  }
  if (work.progress_mode === "owned") {
    return projectOwnedWork(work, ownerById);
  }
  if (work.progress_mode === "waiting") {
    return projectWaitingWork(work, ownerById);
  }
  if (work.progress_mode === "ready") {
    return work.attention_pending
      ? {
          ...workBase(work),
          state: "review",
          relationshipLabel:
            work.status === "needs_input"
              ? "Needs your input"
              : "Brain is reviewing",
          contradiction: false,
        }
      : blockedWork(work, "Next step unavailable");
  }
  return blockedWork(work, "Next step unavailable");
}

function projectOwnedWork(
  work: BrainActiveWork,
  ownerById: ReadonlyMap<string, WorkGraphOwner>,
): MeaningfulWork {
  const sessionId = work.owner_session_id?.trim();
  if (!sessionId || work.owner_delegated !== true) {
    return blockedWork(
      work,
      "No Session assigned",
      placeholderEndpoint(work, "Unassigned"),
    );
  }
  const owner = ownerById.get(sessionId);
  if (!owner || !owner.delegated) {
    return blockedWork(
      work,
      "Session unavailable",
      placeholderEndpoint(work, "Unavailable"),
    );
  }
  const endpoint = agentEndpoint(owner);
  if (owner.status === "failed") {
    return {
      ...workBase(work),
      state: "blocked",
      relationshipLabel: `${owner.label} stopped`,
      endpoint,
      endpointEdgeKind: "ownership",
      sessionId,
      contradiction: false,
    };
  }
  if (owner.status === "blocked" || owner.status === "unknown") {
    return {
      ...workBase(work),
      state: "blocked",
      relationshipLabel:
        owner.status === "blocked"
          ? `${owner.label} is blocked`
          : `${owner.label} is unavailable`,
      endpoint,
      endpointEdgeKind: "ownership",
      sessionId,
      contradiction: owner.status === "unknown",
    };
  }
  if (owner.needsAttention || owner.status === "done") {
    return {
      ...workBase(work),
      state: "review",
      relationshipLabel: `${owner.label} needs review`,
      endpoint,
      endpointEdgeKind: "ownership",
      sessionId,
      contradiction: false,
    };
  }
  return {
    ...workBase(work),
    state: "running",
    relationshipLabel: `${owner.label} owns this`,
    endpoint,
    endpointEdgeKind: "ownership",
    sessionId,
    contradiction: false,
  };
}

function projectWaitingWork(
  work: BrainActiveWork,
  ownerById: ReadonlyMap<string, WorkGraphOwner>,
): MeaningfulWork {
  if (!work.wake) {
    return blockedWork(
      work,
      "Waiting details unavailable",
      placeholderEndpoint(work, "Unknown wait"),
    );
  }
  if (work.wake.kind === "user_input") {
    return waitingWork(work, "Waiting for you", {
      key: "you",
      title: "You",
      kind: "wake",
      state: "waiting",
    });
  }
  if (work.wake.kind === "calendar_result") {
    return waitingWork(work, "Waiting for Calendar", {
      key: "calendar",
      title: "Calendar",
      kind: "wake",
      state: "waiting",
    });
  }
  const owner = ownerFromSessionWakeRef(work.wake.ref, ownerById);
  if (!owner) {
    return blockedWork(
      work,
      "Waiting for unavailable Session",
      placeholderEndpoint(work, "Unavailable"),
      "wait",
    );
  }
  return waitingWork(
    work,
    `Waiting for ${owner.label}`,
    agentEndpoint(owner),
    owner.sessionId,
  );
}

function projectTerminalWork(
  work: BrainActiveWork,
  ownerById: ReadonlyMap<string, WorkGraphOwner>,
): MeaningfulWork | null {
  const finalizations = work.session_finalizations ?? [];
  const failed = finalizations.find((item) => item.state === "failed");
  if (failed) {
    const endpoint = endpointFromFinalization(work, failed, ownerById);
    return {
      ...workBase(work),
      state: "blocked",
      relationshipLabel:
        endpoint.kind === "agent"
          ? `${endpoint.title} could not close`
          : "Session could not close",
      endpoint,
      endpointEdgeKind: "blocked",
      sessionId: endpoint.sessionId,
      contradiction: false,
    };
  }
  const pending = finalizations.find((item) => item.state === "pending");
  if (pending) {
    const endpoint = endpointFromFinalization(work, pending, ownerById);
    return {
      ...workBase(work),
      state: "running",
      relationshipLabel:
        endpoint.kind === "agent"
          ? `${endpoint.title} is closing`
          : "Session is closing",
      endpoint,
      endpointEdgeKind: "ownership",
      sessionId: endpoint.sessionId,
      contradiction: false,
    };
  }
  if (work.attention_pending) {
    return {
      ...workBase(work),
      state: "review",
      relationshipLabel: "Result needs review",
      contradiction: false,
    };
  }
  return null;
}

function waitingWork(
  work: BrainActiveWork,
  relationshipLabel: string,
  endpoint: EndpointProjection,
  sessionId?: string,
): MeaningfulWork {
  return {
    ...workBase(work),
    state: "waiting",
    relationshipLabel,
    endpoint,
    endpointEdgeKind: "wait",
    sessionId,
    contradiction: false,
  };
}

function blockedWork(
  work: BrainActiveWork,
  relationshipLabel: string,
  endpoint?: EndpointProjection,
  endpointEdgeKind: WorkGraphEdgeKind = "blocked",
): MeaningfulWork {
  return {
    ...workBase(work),
    state: "blocked",
    relationshipLabel,
    endpoint,
    endpointEdgeKind: endpoint ? endpointEdgeKind : undefined,
    contradiction: true,
  };
}

function workBase(work: BrainActiveWork) {
  return {
    workId: work.work_id,
    title: work.title,
    terminal: work.status === "done" || work.status === "cancelled",
  };
}

function endpointFromFinalization(
  work: BrainActiveWork,
  finalization: BrainSessionFinalization,
  ownerById: ReadonlyMap<string, WorkGraphOwner>,
): EndpointProjection {
  const owner = ownerById.get(finalization.session_id);
  return owner && owner.delegated
    ? agentEndpoint(owner)
    : placeholderEndpoint(work, "Unavailable");
}

function agentEndpoint(owner: WorkGraphOwner): EndpointProjection {
  return {
    key: `agent:${owner.sessionId}`,
    title: owner.label,
    kind: "agent",
    state: ownerGraphState(owner),
    sessionId: owner.sessionId,
  };
}

function placeholderEndpoint(
  work: BrainActiveWork,
  title: string,
): EndpointProjection {
  return {
    key: `placeholder:${work.work_id}`,
    title,
    kind: "placeholder",
    state: "blocked",
  };
}

function ownerGraphState(owner: WorkGraphOwner): WorkGraphState {
  if (
    owner.status === "failed" ||
    owner.status === "blocked" ||
    owner.status === "unknown"
  ) {
    return "blocked";
  }
  if (owner.needsAttention || owner.status === "done") return "review";
  return "running";
}

function ownerFromSessionWakeRef(
  ref: string,
  ownerById: ReadonlyMap<string, WorkGraphOwner>,
): WorkGraphOwner | undefined {
  let match: WorkGraphOwner | undefined;
  ownerById.forEach((owner) => {
    if (
      ref.startsWith(`session:${owner.sessionId}:turn:`) &&
      (!match || owner.sessionId.length > match.sessionId.length)
    ) {
      match = owner;
    }
  });
  return match;
}

function workNodeFromProjection(work: MeaningfulWork): WorkGraphWorkNode {
  const endpointId = work.endpoint ? endpointIdFromProjection(work.endpoint) : undefined;
  const stateLabel = STATE_LABEL[work.state];
  return {
    id: `work:${work.workId}`,
    kind: "work",
    title: work.title,
    state: work.state,
    stateLabel,
    relationshipLabel: work.relationshipLabel,
    workId: work.workId,
    endpointId,
    sessionId: work.sessionId,
    contradiction: work.contradiction,
    accessibilityLabel: `${work.title}. ${stateLabel}. ${work.relationshipLabel}.`,
  };
}

function endpointNodes(work: readonly MeaningfulWork[]): WorkGraphEndpointNode[] {
  const byId = new Map<string, WorkGraphEndpointNode>();
  work.forEach((item) => {
    const endpoint = item.endpoint;
    if (!endpoint) return;
    const id = endpointIdFromProjection(endpoint);
    if (byId.has(id)) return;
    const stateLabel = STATE_LABEL[endpoint.state];
    byId.set(id, {
      id,
      kind: "endpoint",
      endpointKind: endpoint.kind,
      title: endpoint.title,
      state: endpoint.state,
      stateLabel,
      sessionId: endpoint.sessionId,
      accessibilityLabel:
        endpoint.kind === "agent"
          ? `${endpoint.title}. Session. ${stateLabel}.`
          : `${endpoint.title}. ${stateLabel}.`,
    });
  });
  return Array.from(byId.values()).sort((left, right) => compareText(left.id, right.id));
}

function endpointIdFromProjection(endpoint: EndpointProjection): string {
  return `endpoint:${endpoint.key}`;
}

function aggregateNode(
  hiddenCount: number,
  page: number,
  pageCount: number,
): WorkGraphAggregateNode {
  return {
    id: "aggregate",
    kind: "aggregate",
    title: `+${hiddenCount} more`,
    hiddenCount,
    page,
    pageCount,
    accessibilityLabel: `${hiddenCount} more Work items. Show the next graph view. Page ${page + 1} of ${pageCount}.`,
  };
}

function compareMeaningfulWork(left: MeaningfulWork, right: MeaningfulWork): number {
  const leftRank = left.terminal
    ? TERMINAL_STATE_RANK[left.state]
    : LIVE_STATE_RANK[left.state];
  const rightRank = right.terminal
    ? TERMINAL_STATE_RANK[right.state]
    : LIVE_STATE_RANK[right.state];
  const rank = leftRank - rightRank;
  return rank || compareText(left.workId, right.workId);
}

function compareText(left: string, right: string): number {
  return left < right ? -1 : left > right ? 1 : 0;
}

function normalizedVisibleLimit(value: number | undefined): number {
  if (!Number.isFinite(value)) return WORK_GRAPH_VISIBLE_WORK_LIMIT;
  return Math.max(1, Math.min(6, Math.floor(value!)));
}

function normalizedPage(page: number, pageCount: number): number {
  if (!Number.isFinite(page) || pageCount <= 1) return 0;
  return ((Math.floor(page) % pageCount) + pageCount) % pageCount;
}

function cyclicPage<T>(items: readonly T[], page: number, limit: number): T[] {
  if (items.length <= limit) return [...items];
  const start = (page * limit) % items.length;
  return Array.from(
    { length: limit },
    (_, index) => items[(start + index) % items.length]!,
  );
}

function stateSummary(items: readonly MeaningfulWork[]): string {
  return (["running", "waiting", "review", "blocked"] as const)
    .map((state) => {
      const count = items.filter((item) => item.state === state).length;
      return count > 0 ? `${count} ${STATE_LABEL[state]}` : null;
    })
    .filter(Boolean)
    .join(", ");
}

function countLabel(count: number, noun: string): string {
  return `${count} ${noun}${count === 1 ? "" : "s"}`;
}

function finiteDimension(value: number): number {
  return Number.isFinite(value) ? Math.max(0, value) : 0;
}

function verticalSlotCenters(
  height: number,
  count: number,
  minimumNodeHeight: number,
): number[] {
  if (count <= 0) return [];
  const padding = Math.max(5, Math.min(10, height * 0.025));
  const first = padding + minimumNodeHeight / 2;
  const last = Math.max(first, height - padding - minimumNodeHeight / 2);
  if (count === 1) return [height / 2];
  const step = (last - first) / (count - 1);
  return Array.from({ length: count }, (_, index) => first + step * index);
}

function nodeLayout(
  node: WorkGraphNode,
  x: number,
  y: number,
  width: number,
  height: number,
): WorkGraphNodeLayout {
  return {
    node,
    x,
    y,
    width,
    height,
    centerX: x + width / 2,
    centerY: y + height / 2,
  };
}

function layoutEdge(
  edge: WorkGraphEdge,
  nodes: ReadonlyMap<string, WorkGraphNodeLayout>,
): WorkGraphEdgeLayout | null {
  const from = nodes.get(edge.from);
  const to = nodes.get(edge.to);
  if (!from || !to) return null;
  const startX = from.x + from.width;
  const startY = from.centerY;
  const endX = to.x;
  const endY = to.centerY;
  const controlOffset = Math.max(8, (endX - startX) * 0.48);
  const path = [
    `M ${round(startX)} ${round(startY)}`,
    `C ${round(startX + controlOffset)} ${round(startY)}`,
    `${round(endX - controlOffset)} ${round(endY)}`,
    `${round(endX)} ${round(endY)}`,
  ].join(" ");
  return { ...edge, path, startX, startY, endX, endY };
}

function round(value: number): number {
  return Math.round(value * 10) / 10;
}

function endpointSelectionDetail(
  node: WorkGraphEndpointNode,
  edges: readonly WorkGraphEdge[],
): WorkGraphSelectionDetail {
  const connected = edges.filter((edge) => edge.to === node.id);
  const ownedCount = connected.filter((edge) => edge.kind === "ownership").length;
  const waitingCount = connected.filter((edge) => edge.kind === "wait").length;
  const blockedCount = connected.filter((edge) => edge.kind === "blocked").length;
  const parts = [
    ownedCount > 0 ? `Owns ${countLabel(ownedCount, "Work item")}` : null,
    waitingCount > 0
      ? `${countLabel(waitingCount, "Work item")} waiting here`
      : null,
    blockedCount > 0 ? `${countLabel(blockedCount, "blocked item")}` : null,
  ].filter((part): part is string => Boolean(part));
  return {
    nodeId: node.id,
    title: node.title,
    state: node.state,
    stateLabel: node.stateLabel,
    relationshipLabel: parts.join(" · ") || "Connected to Work",
    sessionId: node.sessionId,
  };
}
