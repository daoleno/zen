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

export type BrainSnapshot = {
  host_agent?: BrainAgentRef | null;
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
    host_agent:
      raw?.host_agent && typeof raw.host_agent === "object"
        ? normalizeAgentRef(raw.host_agent)
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
