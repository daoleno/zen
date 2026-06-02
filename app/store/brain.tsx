import React, { createContext, useContext, useReducer, type ReactNode } from "react";

export type BrainAgentRef = {
  id: string;
  name: string;
  status: string;
  summary?: string;
  cwd?: string;
  command?: string;
  updated_at?: string;
};

export type BrainAdapterCapabilities = {
  native_threads?: boolean;
  native_search?: boolean;
  native_pinning?: boolean;
  native_archive?: boolean;
  native_worktrees?: boolean;
  native_fork?: boolean;
  native_resume?: boolean;
  native_goals?: boolean;
  native_automation?: boolean;
  interactive_tty?: boolean;
  structured_events?: boolean;
};

export type BrainAdapterRef = {
  id: string;
  name: string;
  provider?: string;
  command?: string;
  runtime?: string;
  capabilities?: BrainAdapterCapabilities;
  preferred?: boolean;
};

export type BrainAttentionSummary = {
  pinned?: number;
  needs_review?: number;
  reviewing?: number;
  review_queue?: number;
  active_agents?: number;
  blocked_agents?: number;
  in_flight_agents?: number;
  max_in_flight_agents?: number;
  review_queue_limit?: number;
  available_agent_slots?: number;
  can_start_agent?: boolean;
  backpressure_reason?: string;
  pressure?: string;
};

export type BrainAttentionQueueItem = {
  id: string;
  kind: string;
  title: string;
  summary?: string;
  agent_id?: string;
  thread_id?: string;
  work_item_id?: string;
  status?: string;
  review_state?: string;
  pinned?: boolean;
  project?: string;
  cwd?: string;
  command?: string;
  path?: string;
  updated_at?: string;
};

export type BrainSnapshot = {
  agents?: BrainAgentRef[];
  host_agent?: BrainAgentRef | null;
  host_adapter?: BrainAdapterRef | null;
  adapters?: BrainAdapterRef[];
  chat_thread_id?: string;
  attention?: BrainAttentionSummary;
  attention_queue?: BrainAttentionQueueItem[];
  workspace?: string;
  generated_at?: string;
};

export type BrainServerState = BrainSnapshot & {
  serverId: string;
  serverName: string;
  serverUrl: string;
  hydrated: boolean;
};

export type BrainState = {
  byServer: Record<string, BrainServerState>;
};

export const initialBrainState: BrainState = {
  byServer: {},
};

type RawBrainSnapshot = Partial<BrainSnapshot>;

type Action =
  | {
      type: "BRAIN_SNAPSHOT";
      serverId: string;
      serverName: string;
      serverUrl: string;
      brain: RawBrainSnapshot;
    }
  | { type: "REMOVE_SERVER"; serverId: string };

function normalizeSnapshot(
  raw: RawBrainSnapshot | undefined,
  serverId: string,
  serverName: string,
  serverUrl: string,
): BrainServerState {
  return {
    serverId,
    serverName,
    serverUrl,
    hydrated: true,
    agents: Array.isArray(raw?.agents)
      ? raw.agents.map(normalizeAgentRef).filter((agent) => agent.id)
      : [],
    host_agent:
      raw?.host_agent && typeof raw.host_agent === "object"
        ? normalizeAgentRef(raw.host_agent)
        : undefined,
    host_adapter:
      raw?.host_adapter && typeof raw.host_adapter === "object"
        ? normalizeAdapterRef(raw.host_adapter)
        : undefined,
    adapters: Array.isArray(raw?.adapters)
      ? raw.adapters.map(normalizeAdapterRef).filter((adapter) => adapter.id)
      : [],
    chat_thread_id:
      typeof raw?.chat_thread_id === "string"
        ? raw.chat_thread_id
        : undefined,
    attention: normalizeAttentionSummary(raw?.attention),
    attention_queue: Array.isArray(raw?.attention_queue)
      ? raw.attention_queue
          .map(normalizeAttentionQueueItem)
          .filter((item) => item.id)
      : undefined,
    workspace: typeof raw?.workspace === "string" ? raw.workspace : undefined,
    generated_at:
      typeof raw?.generated_at === "string" ? raw.generated_at : undefined,
  };
}

function normalizeAgentRef(raw: any): BrainAgentRef {
  return {
    id: typeof raw?.id === "string" ? raw.id : "",
    name: typeof raw?.name === "string" ? raw.name : "",
    status: typeof raw?.status === "string" ? raw.status : "unknown",
    summary: typeof raw?.summary === "string" ? raw.summary : undefined,
    cwd: typeof raw?.cwd === "string" ? raw.cwd : undefined,
    command: typeof raw?.command === "string" ? raw.command : undefined,
    updated_at:
      typeof raw?.updated_at === "string" ? raw.updated_at : undefined,
  };
}

function normalizeAdapterRef(raw: any): BrainAdapterRef {
  return {
    id: typeof raw?.id === "string" ? raw.id : "",
    name: typeof raw?.name === "string" ? raw.name : "",
    provider:
      typeof raw?.provider === "string" ? raw.provider : undefined,
    command: typeof raw?.command === "string" ? raw.command : undefined,
    runtime: typeof raw?.runtime === "string" ? raw.runtime : undefined,
    capabilities: normalizeAdapterCapabilities(raw?.capabilities),
    preferred:
      typeof raw?.preferred === "boolean" ? raw.preferred : undefined,
  };
}

function normalizeAdapterCapabilities(raw: any): BrainAdapterCapabilities {
  const source = raw && typeof raw === "object" ? raw : {};
  return {
    native_threads:
      typeof source.native_threads === "boolean" ? source.native_threads : undefined,
    native_search:
      typeof source.native_search === "boolean" ? source.native_search : undefined,
    native_pinning:
      typeof source.native_pinning === "boolean" ? source.native_pinning : undefined,
    native_archive:
      typeof source.native_archive === "boolean" ? source.native_archive : undefined,
    native_worktrees:
      typeof source.native_worktrees === "boolean" ? source.native_worktrees : undefined,
    native_fork:
      typeof source.native_fork === "boolean" ? source.native_fork : undefined,
    native_resume:
      typeof source.native_resume === "boolean" ? source.native_resume : undefined,
    native_goals:
      typeof source.native_goals === "boolean" ? source.native_goals : undefined,
    native_automation:
      typeof source.native_automation === "boolean" ? source.native_automation : undefined,
    interactive_tty:
      typeof source.interactive_tty === "boolean" ? source.interactive_tty : undefined,
    structured_events:
      typeof source.structured_events === "boolean" ? source.structured_events : undefined,
  };
}

function normalizeAttentionSummary(raw: any): BrainAttentionSummary {
  const source = raw && typeof raw === "object" ? raw : {};
  return {
    pinned: normalizeCount(source.pinned),
    needs_review: normalizeCount(source.needs_review),
    reviewing: normalizeCount(source.reviewing),
    review_queue: normalizeCount(source.review_queue),
    active_agents: normalizeCount(source.active_agents),
    blocked_agents: normalizeCount(source.blocked_agents),
    in_flight_agents: normalizeCount(source.in_flight_agents),
    max_in_flight_agents: normalizeCount(source.max_in_flight_agents),
    review_queue_limit: normalizeCount(source.review_queue_limit),
    available_agent_slots: normalizeCount(source.available_agent_slots),
    can_start_agent:
      typeof source.can_start_agent === "boolean"
        ? source.can_start_agent
        : undefined,
    backpressure_reason:
      typeof source.backpressure_reason === "string"
        ? source.backpressure_reason
        : undefined,
    pressure: typeof source.pressure === "string" ? source.pressure : undefined,
  };
}

function normalizeAttentionQueueItem(raw: any): BrainAttentionQueueItem {
  return {
    id: typeof raw?.id === "string" ? raw.id : "",
    kind: typeof raw?.kind === "string" ? raw.kind : "",
    title: typeof raw?.title === "string" ? raw.title : "",
    summary: typeof raw?.summary === "string" ? raw.summary : undefined,
    agent_id: typeof raw?.agent_id === "string" ? raw.agent_id : undefined,
    thread_id: typeof raw?.thread_id === "string" ? raw.thread_id : undefined,
    work_item_id:
      typeof raw?.work_item_id === "string" ? raw.work_item_id : undefined,
    status: typeof raw?.status === "string" ? raw.status : undefined,
    review_state:
      typeof raw?.review_state === "string" ? raw.review_state : undefined,
    pinned: typeof raw?.pinned === "boolean" ? raw.pinned : undefined,
    project: typeof raw?.project === "string" ? raw.project : undefined,
    cwd: typeof raw?.cwd === "string" ? raw.cwd : undefined,
    command: typeof raw?.command === "string" ? raw.command : undefined,
    path: typeof raw?.path === "string" ? raw.path : undefined,
    updated_at:
      typeof raw?.updated_at === "string" ? raw.updated_at : undefined,
  };
}

function normalizeCount(value: unknown) {
  return typeof value === "number" && Number.isFinite(value)
    ? Math.max(0, Math.floor(value))
    : 0;
}

function brainReducer(state: BrainState, action: Action): BrainState {
  switch (action.type) {
    case "BRAIN_SNAPSHOT": {
      const next = normalizeSnapshot(
        action.brain,
        action.serverId,
        action.serverName,
        action.serverUrl,
      );
      return {
        ...state,
        byServer: {
          ...state.byServer,
          [action.serverId]: next,
        },
      };
    }
    case "REMOVE_SERVER": {
      const byServer = { ...state.byServer };
      delete byServer[action.serverId];
      return { byServer };
    }
    default:
      return state;
  }
}

const BrainContext = createContext<{
  state: BrainState;
  dispatch: React.Dispatch<Action>;
} | null>(null);

export function BrainProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(brainReducer, initialBrainState);
  return (
    <BrainContext.Provider value={{ state, dispatch }}>
      {children}
    </BrainContext.Provider>
  );
}

export function useBrain() {
  const ctx = useContext(BrainContext);
  if (!ctx) {
    throw new Error("useBrain must be used within BrainProvider");
  }
  return ctx;
}
