import React, {
  createContext,
  useContext,
  useReducer,
  type ReactNode,
} from "react";
import {
  normalizeAgentSessionCapabilities,
  type AgentSessionCapabilities,
} from "../services/providers/sessionCapabilities";

export type BrainScheduledResult = {
  id: string;
  thread_id: string;
  body: string;
  created_at: string;
  status: string;
  title: string;
  calendar_item_id: string;
  calendar_run_id: string;
  scheduled_for: string;
};

export type BrainWorkStatus =
  | "open"
  | "running"
  | "waiting"
  | "needs_input"
  | "done"
  | "cancelled";

export type BrainWorkWake = {
  kind: "session_terminal" | "calendar_result" | "user_input";
  ref: string;
};

export type BrainWorkProgressMode = "owned" | "waiting" | "ready";

export type BrainSessionFinalization = {
  session_id: string;
  delegated: boolean;
  state: "pending" | "failed" | "complete" | "skipped";
  attempts?: number;
  last_error?: string;
  updated_at: string;
};

export type BrainWorkAttentionState = "queued" | "reviewing";

export type BrainCurrentWork = {
  work_id: string;
  revision: number;
  title: string;
  status: BrainWorkStatus;
  progress_mode?: BrainWorkProgressMode;
  owner_session_id?: string;
  owner_delegated?: boolean;
  wait_for?: string;
  wake?: BrainWorkWake;
  attention_state?: BrainWorkAttentionState;
  session_finalizations?: BrainSessionFinalization[];
  unread_result: boolean;
};

export type BrainWorkBacklog = {
  total: number;
  queued_attention: number;
  historical_results: number;
  repair_needed: number;
};

export type BrainAgentRef = {
  id: string;
  name: string;
  status: string;
  summary?: string;
  cwd?: string;
  command?: string;
  started_at?: number;
  process_id?: number;
  updated_at?: string;
  delegated?: boolean;
  /**
   * Daemon-authoritative flat Session capabilities on brain_snapshot.host_agent.
   * Hidden hosts are absent from agent_session_list — never invent from name/command.
   */
  capabilities?: AgentSessionCapabilities;
};

export type BrainAdapterCapabilities = {
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
  host?: boolean;
  delegated?: boolean;
};

export type BrainSnapshot = {
  agents?: BrainAgentRef[];
  host_agent?: BrainAgentRef | null;
  host_adapter?: BrainAdapterRef | null;
  delegated_adapter?: BrainAdapterRef | null;
  adapters?: BrainAdapterRef[];
  chat_thread_id?: string;
  scheduled_results?: BrainScheduledResult[];
  current_work?: BrainCurrentWork[];
  work_backlog?: BrainWorkBacklog;
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

type RawBrainSnapshot = Omit<
  Partial<BrainSnapshot>,
  "scheduled_results" | "current_work" | "work_backlog"
> & {
  scheduled_results?: unknown[];
  current_work?: unknown[];
  work_backlog?: unknown;
  host_executor?: BrainAdapterRef | null;
  delegated_executor?: BrainAdapterRef | null;
  executors?: BrainAdapterRef[];
};

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
  const hostAdapter = raw?.host_adapter ?? raw?.host_executor;
  const delegatedAdapter = raw?.delegated_adapter ?? raw?.delegated_executor;
  const adapters = Array.isArray(raw?.adapters)
    ? raw.adapters
    : Array.isArray(raw?.executors)
      ? raw.executors
      : [];
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
      hostAdapter && typeof hostAdapter === "object"
        ? normalizeAdapterRef(hostAdapter)
        : undefined,
    delegated_adapter:
      delegatedAdapter && typeof delegatedAdapter === "object"
        ? normalizeAdapterRef(delegatedAdapter)
        : undefined,
    adapters: adapters.map(normalizeAdapterRef).filter((adapter) => adapter.id),
    chat_thread_id:
      typeof raw?.chat_thread_id === "string" ? raw.chat_thread_id : undefined,
    scheduled_results: Array.isArray(raw?.scheduled_results)
      ? normalizeScheduledResults(raw.scheduled_results)
      : [],
    current_work: Array.isArray(raw?.current_work)
      ? normalizeCurrentWork(raw.current_work)
      : [],
    work_backlog: normalizeWorkBacklog(raw?.work_backlog),
    workspace: typeof raw?.workspace === "string" ? raw.workspace : undefined,
    generated_at:
      typeof raw?.generated_at === "string" ? raw.generated_at : undefined,
  };
}

function normalizeCurrentWork(raw: unknown[]): BrainCurrentWork[] {
  const byId = new Map<string, BrainCurrentWork>();
  raw.forEach((value) => {
    const item =
      value && typeof value === "object"
        ? (value as Record<string, unknown>)
        : null;
    if (!item) {
      return;
    }
    const workId =
      typeof item.work_id === "string" ? item.work_id.trim() : "";
    const title = typeof item.title === "string" ? item.title.trim() : "";
    const status = normalizeWorkStatus(item.status);
    const progressMode = normalizeWorkProgressMode(item.progress_mode);
    const terminal = status === "done" || status === "cancelled";
    if (!workId || !title || !status || (!terminal && !progressMode)) {
      return;
    }
    byId.set(workId, {
      work_id: workId,
      revision:
        typeof item.revision === "number" &&
        Number.isSafeInteger(item.revision) &&
        item.revision >= 0
          ? item.revision
          : 0,
      title,
      status,
      progress_mode: progressMode,
      owner_session_id:
        typeof item.owner_session_id === "string" &&
        item.owner_session_id.trim()
          ? item.owner_session_id.trim()
          : undefined,
      owner_delegated: item.owner_delegated === true ? true : undefined,
      wait_for:
        typeof item.wait_for === "string" && item.wait_for.trim()
          ? item.wait_for.trim()
          : undefined,
      wake: normalizeWorkWake(item.wake),
      attention_state: normalizeWorkAttentionState(item.attention_state),
      session_finalizations: normalizeSessionFinalizations(
        item.session_finalizations,
      ),
      unread_result: item.unread_result === true,
    });
  });
  return Array.from(byId.values());
}

function normalizeWorkAttentionState(
  value: unknown,
): BrainWorkAttentionState | undefined {
  return value === "queued" || value === "reviewing" ? value : undefined;
}

function normalizeWorkBacklog(value: unknown): BrainWorkBacklog {
  const backlog =
    value && typeof value === "object"
      ? (value as Record<string, unknown>)
      : {};
  const count = (candidate: unknown) =>
    typeof candidate === "number" &&
    Number.isSafeInteger(candidate) &&
    candidate >= 0
      ? candidate
      : 0;
  return {
    total: count(backlog.total),
    queued_attention: count(backlog.queued_attention),
    historical_results: count(backlog.historical_results),
    repair_needed: count(backlog.repair_needed),
  };
}

function normalizeWorkProgressMode(
  value: unknown,
): BrainWorkProgressMode | undefined {
  return value === "owned" || value === "waiting" || value === "ready"
    ? value
    : undefined;
}

function normalizeSessionFinalizations(
  value: unknown,
): BrainSessionFinalization[] | undefined {
  if (!Array.isArray(value)) {
    return undefined;
  }
  const bySession = new Map<string, BrainSessionFinalization>();
  value.forEach((candidate) => {
    const finalization = normalizeSessionFinalization(candidate);
    if (finalization) {
      bySession.set(finalization.session_id, finalization);
    }
  });
  return bySession.size > 0 ? Array.from(bySession.values()) : undefined;
}

function normalizeWorkWake(value: unknown): BrainWorkWake | undefined {
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const wake = value as Record<string, unknown>;
  const ref = typeof wake.ref === "string" ? wake.ref.trim() : "";
  if (!ref) {
    return undefined;
  }
  switch (wake.kind) {
    case "session_terminal":
    case "calendar_result":
    case "user_input":
      return { kind: wake.kind, ref };
    default:
      return undefined;
  }
}

function normalizeSessionFinalization(
  value: unknown,
): BrainSessionFinalization | undefined {
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const finalization = value as Record<string, unknown>;
  const sessionId =
    typeof finalization.session_id === "string"
      ? finalization.session_id.trim()
      : "";
  const updatedAt =
    typeof finalization.updated_at === "string"
      ? finalization.updated_at.trim()
      : "";
  const delegated = finalization.delegated === true;
  const state = finalization.state;
  if (
    !sessionId ||
    !updatedAt ||
    (state !== "pending" &&
      state !== "failed" &&
      state !== "complete" &&
      state !== "skipped")
  ) {
    return undefined;
  }
  const attempts =
    typeof finalization.attempts === "number" &&
    Number.isSafeInteger(finalization.attempts) &&
    finalization.attempts >= 0
      ? finalization.attempts
      : undefined;
  const lastError =
    typeof finalization.last_error === "string" &&
    finalization.last_error.trim()
      ? finalization.last_error.trim()
      : undefined;
  return {
    session_id: sessionId,
    delegated,
    state,
    attempts,
    last_error: lastError,
    updated_at: updatedAt,
  };
}

function normalizeWorkStatus(value: unknown): BrainWorkStatus | null {
  switch (value) {
    case "open":
    case "running":
    case "waiting":
    case "needs_input":
    case "done":
    case "cancelled":
      return value;
    default:
      return null;
  }
}

function normalizeScheduledResult(raw: any): BrainScheduledResult {
  return {
    id: typeof raw?.id === "string" ? raw.id : "",
    thread_id: typeof raw?.thread_id === "string" ? raw.thread_id : "",
    body: typeof raw?.body === "string" ? raw.body : "",
    created_at: typeof raw?.created_at === "string" ? raw.created_at : "",
    status: typeof raw?.status === "string" ? raw.status : "",
    title: typeof raw?.title === "string" ? raw.title : "",
    calendar_item_id:
      typeof raw?.calendar_item_id === "string" ? raw.calendar_item_id : "",
    calendar_run_id:
      typeof raw?.calendar_run_id === "string" ? raw.calendar_run_id : "",
    scheduled_for:
      typeof raw?.scheduled_for === "string" ? raw.scheduled_for : "",
  };
}

function normalizeScheduledResults(raw: any[]): BrainScheduledResult[] {
  const byId = new Map<string, BrainScheduledResult>();
  raw
    .map(normalizeScheduledResult)
    .filter(
      (result) =>
        result.id &&
        result.thread_id &&
        result.body &&
        result.created_at &&
        result.status &&
        result.title &&
        result.calendar_item_id &&
        result.calendar_run_id &&
        result.scheduled_for,
    )
    .forEach((result) => byId.set(result.id, result));
  return Array.from(byId.values()).sort((left, right) => {
    const leftTime = Date.parse(left.created_at);
    const rightTime = Date.parse(right.created_at);
    if (
      Number.isFinite(leftTime) &&
      Number.isFinite(rightTime) &&
      leftTime !== rightTime
    ) {
      return leftTime - rightTime;
    }
    if (Number.isFinite(leftTime) !== Number.isFinite(rightTime)) {
      return Number.isFinite(leftTime) ? 1 : -1;
    }
    return left.id.localeCompare(right.id);
  });
}

function normalizeAgentRef(raw: any): BrainAgentRef {
  const parsedStartedAt =
    typeof raw?.started_at === "string" ||
    typeof raw?.started_at === "number" ||
    raw?.started_at instanceof Date
      ? new Date(raw.started_at).getTime()
      : Number.NaN;
  const processID =
    typeof raw?.process_id === "number" &&
    Number.isInteger(raw.process_id) &&
    raw.process_id > 0
      ? raw.process_id
      : undefined;

  return {
    id: typeof raw?.id === "string" ? raw.id : "",
    name: typeof raw?.name === "string" ? raw.name : "",
    status: typeof raw?.status === "string" ? raw.status : "unknown",
    summary: typeof raw?.summary === "string" ? raw.summary : undefined,
    cwd: typeof raw?.cwd === "string" ? raw.cwd : undefined,
    command: typeof raw?.command === "string" ? raw.command : undefined,
    started_at: Number.isFinite(parsedStartedAt) ? parsedStartedAt : undefined,
    process_id: processID,
    updated_at:
      typeof raw?.updated_at === "string" ? raw.updated_at : undefined,
    delegated: raw?.delegated === true,
    // Same strict flat boolean helper as agent_session.capabilities.
    capabilities: normalizeAgentSessionCapabilities(raw?.capabilities),
  };
}

function normalizeAdapterRef(raw: any): BrainAdapterRef {
  return {
    id: typeof raw?.id === "string" ? raw.id : "",
    name: typeof raw?.name === "string" ? raw.name : "",
    provider: typeof raw?.provider === "string" ? raw.provider : undefined,
    command: typeof raw?.command === "string" ? raw.command : undefined,
    runtime: typeof raw?.runtime === "string" ? raw.runtime : undefined,
    capabilities: normalizeAdapterCapabilities(raw?.capabilities),
    host: typeof raw?.host === "boolean" ? raw.host : undefined,
    delegated: typeof raw?.delegated === "boolean" ? raw.delegated : undefined,
  };
}

function normalizeAdapterCapabilities(raw: any): BrainAdapterCapabilities {
  const source = raw && typeof raw === "object" ? raw : {};
  return {
    interactive_tty:
      typeof source.interactive_tty === "boolean"
        ? source.interactive_tty
        : undefined,
    structured_events:
      typeof source.structured_events === "boolean"
        ? source.structured_events
        : undefined,
  };
}

export function brainReducer(state: BrainState, action: Action): BrainState {
  switch (action.type) {
    case "BRAIN_SNAPSHOT": {
      const next = normalizeSnapshot(
        action.brain,
        action.serverId,
        action.serverName,
        action.serverUrl,
      );
      const byServer = brainServerStatesEqual(
        state.byServer[action.serverId],
        next,
      )
        ? state.byServer
        : { ...state.byServer, [action.serverId]: next };
      if (byServer === state.byServer) {
        return state;
      }
      return {
        ...state,
        byServer,
      };
    }
    case "REMOVE_SERVER": {
      if (!(action.serverId in state.byServer)) {
        return state;
      }
      const byServer = { ...state.byServer };
      delete byServer[action.serverId];
      return {
        ...state,
        byServer,
      };
    }
    default:
      return state;
  }
}

function brainServerStatesEqual(
  left: BrainServerState | undefined,
  right: BrainServerState,
): boolean {
  if (left === right) {
    return true;
  }
  if (!left) {
    return false;
  }
  return (
    left.serverId === right.serverId &&
    left.serverName === right.serverName &&
    left.serverUrl === right.serverUrl &&
    left.hydrated === right.hydrated &&
    left.chat_thread_id === right.chat_thread_id &&
    left.workspace === right.workspace &&
    agentRefsEqual(left.host_agent, right.host_agent) &&
    adapterRefsEqual(left.host_adapter, right.host_adapter) &&
    adapterRefsEqual(left.delegated_adapter, right.delegated_adapter) &&
    agentRefArraysEqual(left.agents ?? [], right.agents ?? []) &&
    adapterRefArraysEqual(left.adapters ?? [], right.adapters ?? []) &&
    scheduledResultArraysEqual(
      left.scheduled_results ?? [],
      right.scheduled_results ?? [],
    ) &&
    currentWorkArraysEqual(left.current_work ?? [], right.current_work ?? []) &&
    JSON.stringify(left.work_backlog) === JSON.stringify(right.work_backlog)
  );
}

function currentWorkArraysEqual(
  left: BrainCurrentWork[],
  right: BrainCurrentWork[],
) {
  return (
    left.length === right.length &&
    left.every(
      (item, index) => JSON.stringify(item) === JSON.stringify(right[index]),
    )
  );
}

function scheduledResultArraysEqual(
  left: BrainScheduledResult[],
  right: BrainScheduledResult[],
) {
  return (
    left.length === right.length &&
    left.every(
      (message, index) =>
        JSON.stringify(message) === JSON.stringify(right[index]),
    )
  );
}

function agentRefArraysEqual(
  left: BrainAgentRef[],
  right: BrainAgentRef[],
): boolean {
  if (left === right) {
    return true;
  }
  if (left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (!agentRefsEqual(left[index], right[index])) {
      return false;
    }
  }
  return true;
}

function agentRefsEqual(
  left: BrainAgentRef | null | undefined,
  right: BrainAgentRef | null | undefined,
): boolean {
  if (left === right) {
    return true;
  }
  if (!left || !right) {
    return false;
  }
  return (
    left.id === right.id &&
    left.name === right.name &&
    left.status === right.status &&
    left.summary === right.summary &&
    left.cwd === right.cwd &&
    left.command === right.command &&
    left.started_at === right.started_at &&
    left.process_id === right.process_id &&
    left.updated_at === right.updated_at &&
    left.delegated === right.delegated &&
    left.capabilities?.structured_events ===
      right.capabilities?.structured_events &&
    left.capabilities?.model_profile_managed ===
      right.capabilities?.model_profile_managed &&
    left.capabilities?.model_profile_active_switch ===
      right.capabilities?.model_profile_active_switch
  );
}

function adapterRefArraysEqual(
  left: BrainAdapterRef[],
  right: BrainAdapterRef[],
): boolean {
  if (left === right) {
    return true;
  }
  if (left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (!adapterRefsEqual(left[index], right[index])) {
      return false;
    }
  }
  return true;
}

function adapterRefsEqual(
  left: BrainAdapterRef | null | undefined,
  right: BrainAdapterRef | null | undefined,
): boolean {
  if (left === right) {
    return true;
  }
  if (!left || !right) {
    return false;
  }
  return (
    left.id === right.id &&
    left.name === right.name &&
    left.provider === right.provider &&
    left.command === right.command &&
    left.runtime === right.runtime &&
    left.host === right.host &&
    left.delegated === right.delegated &&
    adapterCapabilitiesEqual(left.capabilities, right.capabilities)
  );
}

function adapterCapabilitiesEqual(
  left: BrainAdapterCapabilities | undefined,
  right: BrainAdapterCapabilities | undefined,
): boolean {
  if (left === right) {
    return true;
  }
  if (!left || !right) {
    return false;
  }
  return (
    left.interactive_tty === right.interactive_tty &&
    left.structured_events === right.structured_events
  );
}

const BrainStateContext = createContext<BrainState | null>(null);
const BrainDispatchContext = createContext<React.Dispatch<Action> | null>(null);

export function BrainProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(brainReducer, initialBrainState);
  return (
    <BrainDispatchContext.Provider value={dispatch}>
      <BrainStateContext.Provider value={state}>
        {children}
      </BrainStateContext.Provider>
    </BrainDispatchContext.Provider>
  );
}

export function useBrain() {
  const state = useContext(BrainStateContext);
  const dispatch = useContext(BrainDispatchContext);
  if (!state || !dispatch) {
    throw new Error("useBrain must be used within BrainProvider");
  }
  return { state, dispatch };
}

export function useBrainDispatch() {
  const dispatch = useContext(BrainDispatchContext);
  if (!dispatch) {
    throw new Error("useBrainDispatch must be used within BrainProvider");
  }
  return dispatch;
}
