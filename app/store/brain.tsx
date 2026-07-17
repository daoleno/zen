import React, {
  createContext,
  useContext,
  useReducer,
  type ReactNode,
} from "react";

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

export type BrainAgentRef = {
  id: string;
  name: string;
  status: string;
  summary?: string;
  cwd?: string;
  command?: string;
  updated_at?: string;
  delegated?: boolean;
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

type RawBrainSnapshot = Omit<Partial<BrainSnapshot>, "scheduled_results"> & {
  scheduled_results?: unknown[];
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
      typeof raw?.chat_thread_id === "string"
        ? raw.chat_thread_id
        : undefined,
    scheduled_results: Array.isArray(raw?.scheduled_results)
      ? normalizeScheduledResults(raw.scheduled_results)
      : [],
    workspace: typeof raw?.workspace === "string" ? raw.workspace : undefined,
    generated_at:
      typeof raw?.generated_at === "string" ? raw.generated_at : undefined,
  };
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
      typeof raw?.calendar_item_id === "string"
        ? raw.calendar_item_id
        : "",
    calendar_run_id:
      typeof raw?.calendar_run_id === "string"
        ? raw.calendar_run_id
        : "",
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
    if (Number.isFinite(leftTime) && Number.isFinite(rightTime) && leftTime !== rightTime) {
      return leftTime - rightTime;
    }
    if (Number.isFinite(leftTime) !== Number.isFinite(rightTime)) {
      return Number.isFinite(leftTime) ? 1 : -1;
    }
    return left.id.localeCompare(right.id);
  });
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
    delegated: raw?.delegated === true,
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
    host: typeof raw?.host === "boolean" ? raw.host : undefined,
    delegated:
      typeof raw?.delegated === "boolean" ? raw.delegated : undefined,
  };
}

function normalizeAdapterCapabilities(raw: any): BrainAdapterCapabilities {
  const source = raw && typeof raw === "object" ? raw : {};
  return {
    interactive_tty:
      typeof source.interactive_tty === "boolean" ? source.interactive_tty : undefined,
    structured_events:
      typeof source.structured_events === "boolean" ? source.structured_events : undefined,
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
      const byServer = brainServerStatesEqual(state.byServer[action.serverId], next)
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
    left.updated_at === right.updated_at &&
    left.delegated === right.delegated
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
