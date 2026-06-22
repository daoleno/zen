import React, { createContext, useContext, useReducer, type ReactNode } from "react";

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
  preferred?: boolean;
};

export type BrainSnapshot = {
  agents?: BrainAgentRef[];
  host_agent?: BrainAgentRef | null;
  host_adapter?: BrainAdapterRef | null;
  adapters?: BrainAdapterRef[];
  chat_thread_id?: string;
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
    preferred:
      typeof raw?.preferred === "boolean" ? raw.preferred : undefined,
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
