import AsyncStorage from "@react-native-async-storage/async-storage";
import React, {
  createContext,
  useContext,
  useEffect,
  useReducer,
  type ReactNode,
} from "react";

const brainReadCursorStorageKey = "zen.brain.result_read_cursors.v1";

export type BrainChatMessage = {
  id: string;
  thread_id: string;
  role: string;
  body: string;
  created_at: string;
  kind?: string;
  status?: string;
  title?: string;
  calendar_item_id?: string;
  calendar_run_id?: string;
  scheduled_for?: string;
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
  scheduled_results?: BrainChatMessage[];
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
  readCursors: Record<string, string>;
  unreadByThread: Record<string, number>;
  cursorsHydrated: boolean;
};

export const initialBrainState: BrainState = {
  byServer: {},
  readCursors: {},
  unreadByThread: {},
  cursorsHydrated: false,
};

type RawBrainSnapshot = Partial<BrainSnapshot> & {
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
  | { type: "REMOVE_SERVER"; serverId: string }
  | { type: "BRAIN_READ_CURSORS_HYDRATED"; cursors: Record<string, string> }
  | { type: "BRAIN_THREAD_READ"; serverId: string; threadId: string };

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

function normalizeChatMessage(raw: any): BrainChatMessage {
  return {
    id: typeof raw?.id === "string" ? raw.id : "",
    thread_id: typeof raw?.thread_id === "string" ? raw.thread_id : "",
    role: typeof raw?.role === "string" ? raw.role : "assistant",
    body: typeof raw?.body === "string" ? raw.body : "",
    created_at: typeof raw?.created_at === "string" ? raw.created_at : "",
    kind: typeof raw?.kind === "string" ? raw.kind : undefined,
    status: typeof raw?.status === "string" ? raw.status : undefined,
    title: typeof raw?.title === "string" ? raw.title : undefined,
    calendar_item_id:
      typeof raw?.calendar_item_id === "string"
        ? raw.calendar_item_id
        : undefined,
    calendar_run_id:
      typeof raw?.calendar_run_id === "string"
        ? raw.calendar_run_id
        : undefined,
    scheduled_for:
      typeof raw?.scheduled_for === "string" ? raw.scheduled_for : undefined,
  };
}

function normalizeScheduledResults(raw: any[]): BrainChatMessage[] {
  const byId = new Map<string, BrainChatMessage>();
  raw
    .map(normalizeChatMessage)
    .filter((message) => message.id && message.thread_id)
    .forEach((message) => byId.set(message.id, message));
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
      const unreadByThread = calculateUnread(byServer, state.readCursors);
      if (
        byServer === state.byServer &&
        shallowRecordEqual(unreadByThread, state.unreadByThread)
      ) {
        return state;
      }
      return {
        ...state,
        byServer,
        unreadByThread,
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
        unreadByThread: calculateUnread(byServer, state.readCursors),
      };
    }
    case "BRAIN_READ_CURSORS_HYDRATED": {
      const readCursors = normalizeCursors(action.cursors);
      return {
        ...state,
        readCursors,
        unreadByThread: calculateUnread(state.byServer, readCursors),
        cursorsHydrated: true,
      };
    }
    case "BRAIN_THREAD_READ": {
      const key = brainThreadKey(action.serverId, action.threadId);
      const results = state.byServer[action.serverId]?.scheduled_results ?? [];
      const latest = [...results]
        .reverse()
        .find((message) => message.thread_id === action.threadId);
      if (!latest) return state;
      const cursor = messageCursor(latest);
      if (
        state.readCursors[key] === cursor &&
        state.unreadByThread[key] === 0
      ) {
        return state;
      }
      const readCursors = { ...state.readCursors, [key]: cursor };
      return {
        ...state,
        readCursors,
        unreadByThread: calculateUnread(state.byServer, readCursors),
      };
    }
    default:
      return state;
  }
}

export function brainThreadKey(serverId: string, threadId: string) {
  return `${serverId}:${threadId}`;
}

export function totalBrainUnread(state: BrainState): number {
  return Object.values(state.unreadByThread).reduce(
    (total, count) => total + count,
    0,
  );
}

function messageCursor(message: BrainChatMessage) {
  return `${message.created_at}\u0000${message.id}`;
}

function normalizeCursors(raw: Record<string, string>) {
  return Object.fromEntries(
    Object.entries(raw || {}).filter(
      ([key, value]) => key && typeof value === "string",
    ),
  );
}

function calculateUnread(
  byServer: BrainState["byServer"],
  cursors: Record<string, string>,
) {
  const unread: Record<string, number> = {};
  for (const [serverId, server] of Object.entries(byServer)) {
    const byThread = new Map<string, BrainChatMessage[]>();
    for (const message of server.scheduled_results ?? []) {
      const messages = byThread.get(message.thread_id) ?? [];
      messages.push(message);
      byThread.set(message.thread_id, messages);
    }
    for (const [threadId, messages] of byThread) {
      const key = brainThreadKey(serverId, threadId);
      const cursor = cursors[key];
      if (!cursor) {
        unread[key] = messages.length;
        continue;
      }
      const separator = cursor.indexOf("\u0000");
      const cursorTime = separator >= 0 ? cursor.slice(0, separator) : "";
      const cursorId = separator >= 0 ? cursor.slice(separator + 1) : "";
      const cursorIndex = messages.findIndex(
        (message) => message.id === cursorId,
      );
      const count =
        cursorIndex >= 0
          ? messages.length - cursorIndex - 1
          : messages.filter((message) =>
              isMessageAfterCursor(message, cursorTime, cursorId)
            ).length;
      if (count > 0) unread[key] = count;
    }
  }
  return unread;
}

function isMessageAfterCursor(
  message: BrainChatMessage,
  cursorTime: string,
  cursorId: string,
) {
  const messageTime = Date.parse(message.created_at);
  const parsedCursorTime = Date.parse(cursorTime);
  if (Number.isFinite(messageTime) && Number.isFinite(parsedCursorTime)) {
    if (messageTime !== parsedCursorTime) {
      return messageTime > parsedCursorTime;
    }
    return message.id.localeCompare(cursorId) > 0;
  }
  return messageCursor(message).localeCompare(`${cursorTime}\u0000${cursorId}`) > 0;
}

function shallowRecordEqual(
  left: Record<string, number>,
  right: Record<string, number>,
) {
  const keys = Object.keys(left);
  return (
    keys.length === Object.keys(right).length &&
    keys.every((key) => left[key] === right[key])
  );
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
    chatMessageArraysEqual(
      left.scheduled_results ?? [],
      right.scheduled_results ?? [],
    )
  );
}

function chatMessageArraysEqual(
  left: BrainChatMessage[],
  right: BrainChatMessage[],
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
  useEffect(() => {
    let cancelled = false;
    void AsyncStorage.getItem(brainReadCursorStorageKey).then((value) => {
      if (cancelled) return;
      try {
        dispatch({
          type: "BRAIN_READ_CURSORS_HYDRATED",
          cursors: JSON.parse(value || "{}"),
        });
      } catch {
        dispatch({ type: "BRAIN_READ_CURSORS_HYDRATED", cursors: {} });
      }
    });
    return () => {
      cancelled = true;
    };
  }, []);
  useEffect(() => {
    if (!state.cursorsHydrated) return;
    void AsyncStorage.setItem(brainReadCursorStorageKey, JSON.stringify(state.readCursors));
  }, [state.cursorsHydrated, state.readCursors]);
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
