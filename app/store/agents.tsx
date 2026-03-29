import React, { createContext, useContext, useReducer, ReactNode } from 'react';
import { AgentStatus } from '../constants/tokens';
import { makeSessionKey } from '../services/sessionKeys';

export interface Agent {
  key: string;
  id: string;
  serverId: string;
  serverName: string;
  serverUrl: string;
  name: string;
  status: AgentStatus;
  project?: string;
  summary: string;
  last_output_lines: string[];
  updated_at: number;
}

export type ConnectionState = 'offline' | 'connecting' | 'connected';

interface State {
  agents: Agent[];
  serverConnections: Record<string, ConnectionState>;
  selectedAgentKey: string | null;
}

type RawAgent = {
  id: string;
  name: string;
  status: AgentStatus;
  project?: string;
  summary?: string;
  last_output_lines?: string[];
  updated_at?: string | number | Date;
};

type Action =
  | {
      type: 'UPSERT_SERVER_AGENTS';
      serverId: string;
      serverName: string;
      serverUrl: string;
      agents: RawAgent[];
    }
  | { type: 'UPDATE_OUTPUT'; serverId: string; agent_id: string; lines: string[] }
  | { type: 'STATE_CHANGE'; serverId: string; agent_id: string; old: string; new_state: string }
  | { type: 'SET_SERVER_CONNECTION_STATE'; serverId: string; connectionState: ConnectionState }
  | { type: 'REMOVE_SERVER'; serverId: string }
  | { type: 'SELECT_AGENT'; key: string | null };

const initialState: State = {
  agents: [],
  serverConnections: {},
  selectedAgentKey: null,
};

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'UPSERT_SERVER_AGENTS': {
      const nextAgents = action.agents.map(agent =>
        normalizeAgent(agent, action.serverId, action.serverName, action.serverUrl),
      );

      return {
        ...state,
        agents: [
          ...state.agents.filter(agent => agent.serverId !== action.serverId),
          ...nextAgents,
        ],
      };
    }
    case 'UPDATE_OUTPUT': {
      const targetKey = makeSessionKey(action.serverId, action.agent_id);
      const agents = state.agents.map(agent => {
        if (agent.key !== targetKey) return agent;
        return {
          ...agent,
          last_output_lines: [...agent.last_output_lines, ...action.lines].slice(-1000),
          updated_at: Date.now(),
        };
      });
      return { ...state, agents };
    }
    case 'STATE_CHANGE': {
      const targetKey = makeSessionKey(action.serverId, action.agent_id);
      const agents = state.agents.map(agent => {
        if (agent.key !== targetKey) return agent;
        return {
          ...agent,
          status: action.new_state as AgentStatus,
          updated_at: Date.now(),
        };
      });
      return { ...state, agents };
    }
    case 'SET_SERVER_CONNECTION_STATE':
      return {
        ...state,
        serverConnections: {
          ...state.serverConnections,
          [action.serverId]: action.connectionState,
        },
      };
    case 'REMOVE_SERVER':
      return {
        ...state,
        agents: state.agents.filter(agent => agent.serverId !== action.serverId),
        serverConnections: Object.fromEntries(
          Object.entries(state.serverConnections).filter(([serverId]) => serverId !== action.serverId),
        ),
        selectedAgentKey:
          state.selectedAgentKey && parseServerIdFromKey(state.selectedAgentKey) === action.serverId
            ? null
            : state.selectedAgentKey,
      };
    case 'SELECT_AGENT':
      return { ...state, selectedAgentKey: action.key };
    default:
      return state;
  }
}

function normalizeAgent(
  agent: RawAgent,
  serverId: string,
  serverName: string,
  serverUrl: string,
): Agent {
  return {
    key: makeSessionKey(serverId, agent.id),
    id: agent.id,
    serverId,
    serverName,
    serverUrl,
    name: agent.name,
    status: agent.status,
    project: agent.project,
    summary: agent.summary || '',
    last_output_lines: Array.isArray(agent.last_output_lines) ? agent.last_output_lines : [],
    updated_at: normalizeTimestamp(agent.updated_at),
  };
}

function normalizeTimestamp(value: RawAgent['updated_at']): number {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return value > 10_000_000_000 ? value : value * 1000;
  }

  if (typeof value === 'string') {
    const parsed = Date.parse(value);
    if (!Number.isNaN(parsed)) return parsed;
  }

  if (value instanceof Date) {
    return value.getTime();
  }

  return Date.now();
}

function parseServerIdFromKey(sessionKey: string): string | null {
  try {
    const [serverId] = JSON.parse(sessionKey) as [string, string];
    return typeof serverId === 'string' ? serverId : null;
  } catch {
    return null;
  }
}

const AgentContext = createContext<{ state: State; dispatch: React.Dispatch<Action> } | null>(null);

export function AgentProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, initialState);
  return (
    <AgentContext.Provider value={{ state, dispatch }}>
      {children}
    </AgentContext.Provider>
  );
}

export function useAgents() {
  const ctx = useContext(AgentContext);
  if (!ctx) throw new Error('useAgents must be used within AgentProvider');
  return ctx;
}
