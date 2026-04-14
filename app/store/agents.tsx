import React, { createContext, useContext, useReducer, ReactNode } from 'react';
import { AgentStatus } from '../constants/tokens';
import type { ConnectionIssue } from '../services/connectionIssue';
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
  cwd?: string;
  command?: string;
  summary: string;
  last_output_lines: string[];
  updated_at: number;
}

export type ConnectionState = 'offline' | 'connecting' | 'connected';

interface State {
  agents: Agent[];
  serverConnections: Record<string, ConnectionState>;
  serverConnectionIssues: Record<string, ConnectionIssue | null>;
  hydratedServers: Record<string, boolean>;
}

type RawAgent = {
  id: string;
  name: string;
  status: AgentStatus;
  project?: string;
  cwd?: string;
  command?: string;
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
  | {
      type: 'UPSERT_AGENT';
      serverId: string;
      serverName: string;
      serverUrl: string;
      agent: RawAgent;
    }
  | { type: 'REMOVE_AGENT'; serverId: string; agent_id: string }
  | { type: 'SET_SERVER_CONNECTION_STATE'; serverId: string; connectionState: ConnectionState }
  | { type: 'SET_SERVER_CONNECTION_ISSUE'; serverId: string; issue: ConnectionIssue | null }
  | { type: 'REMOVE_SERVER'; serverId: string };

const initialState: State = {
  agents: [],
  serverConnections: {},
  serverConnectionIssues: {},
  hydratedServers: {},
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
        hydratedServers: {
          ...state.hydratedServers,
          [action.serverId]: true,
        },
      };
    }
    case 'UPSERT_AGENT': {
      const nextAgent = normalizeAgent(action.agent, action.serverId, action.serverName, action.serverUrl);
      const exists = state.agents.some(agent => agent.key === nextAgent.key);
      return {
        ...state,
        agents: exists
          ? state.agents.map(agent => (agent.key === nextAgent.key ? nextAgent : agent))
          : [...state.agents, nextAgent],
        hydratedServers: {
          ...state.hydratedServers,
          [action.serverId]: true,
        },
      };
    }
    case 'REMOVE_AGENT': {
      const targetKey = makeSessionKey(action.serverId, action.agent_id);
      return {
        ...state,
        agents: state.agents.filter(agent => agent.key !== targetKey),
      };
    }
    case 'SET_SERVER_CONNECTION_STATE':
      return {
        ...state,
        serverConnections: {
          ...state.serverConnections,
          [action.serverId]: action.connectionState,
        },
      };
    case 'SET_SERVER_CONNECTION_ISSUE':
      return {
        ...state,
        serverConnectionIssues: {
          ...state.serverConnectionIssues,
          [action.serverId]: action.issue,
        },
      };
    case 'REMOVE_SERVER':
      return {
        ...state,
        agents: state.agents.filter(agent => agent.serverId !== action.serverId),
        serverConnections: Object.fromEntries(
          Object.entries(state.serverConnections).filter(([serverId]) => serverId !== action.serverId),
        ),
        serverConnectionIssues: Object.fromEntries(
          Object.entries(state.serverConnectionIssues).filter(([serverId]) => serverId !== action.serverId),
        ),
        hydratedServers: Object.fromEntries(
          Object.entries(state.hydratedServers).filter(([serverId]) => serverId !== action.serverId),
        ),
      };
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
    cwd: agent.cwd,
    command: agent.command,
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
