import React, { createContext, useContext, useReducer, ReactNode } from 'react';
import { AgentStatus } from '../constants/tokens';
import type { ConnectionIssue } from '../services/connectionIssue';
import type { ServerLatencySample } from '../services/serverLatency';
import { makeSessionKey } from '../services/sessionKeys';
import {
  bumpServerConnectionGeneration,
  isAgentSessionListFreshForConnection as isAgentSessionListFreshForConnectionInput,
  stampAgentSessionListGeneration as stampAgentSessionListGenerationForServer,
} from '../services/agentSessionListTransport';
import {
  normalizeAgentSessionCapabilities,
  type AgentSessionCapabilities,
} from '../services/providers/sessionCapabilities';

export type AgentCapabilities = AgentSessionCapabilities;

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
  phase?: string;
  attention?: string;
  task_class?: string;
  event_kind?: string;
  details_json?: string;
  needs_attention?: boolean;
  last_output_lines: string[];
  started_at?: number;
  updated_at: number;
  process_id?: number;
  delegated?: boolean;
  capabilities?: AgentCapabilities;
}

export type ConnectionState = 'offline' | 'connecting' | 'connected';

export interface State {
  agents: Agent[];
  serverConnections: Record<string, ConnectionState>;
  serverConnectionIssues: Record<string, ConnectionIssue | null>;
  serverLatencyById: Record<string, ServerLatencySample | undefined>;
  hydratedServers: Record<string, boolean>;
  /**
   * Transport-owned: increments when a server enters `connected`.
   * Used with agentSessionListGenerationByServer to prove a full
   * agent_session_list arrived for the current WebSocket generation.
   */
  connectionGenerationByServer: Record<string, number>;
  /** Set to connectionGeneration when UPSERT_SERVER_AGENTS arrives while connected. */
  agentSessionListGenerationByServer: Record<string, number>;
}

export type RawAgent = {
  id: string;
  name: string;
  status: AgentStatus;
  project?: string;
  cwd?: string;
  command?: string;
  summary?: string;
  phase?: string;
  attention?: string;
  task_class?: string;
  event_kind?: string;
  details_json?: string;
  needs_attention?: boolean;
  last_output_lines?: string[];
  started_at?: string | number | Date;
  updated_at?: string | number | Date;
  process_id?: number;
  delegated?: boolean;
  capabilities?: {
    structured_events?: unknown;
    model_profile_managed?: unknown;
    model_profile_active_switch?: unknown;
  };
};

export type Action =
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
  | { type: 'SET_SERVER_LATENCY'; serverId: string; sample: ServerLatencySample }
  | { type: 'REMOVE_SERVER'; serverId: string };

export const initialAgentState: State = {
  agents: [],
  serverConnections: {},
  serverConnectionIssues: {},
  serverLatencyById: {},
  hydratedServers: {},
  connectionGenerationByServer: {},
  agentSessionListGenerationByServer: {},
};

export function agentReducer(state: State, action: Action): State {
  switch (action.type) {
    case 'UPSERT_SERVER_AGENTS': {
      const previousServerAgents = state.agents.filter(agent => agent.serverId === action.serverId);
      const previousByKey = new Map(previousServerAgents.map(agent => [agent.key, agent]));
      let agentsChanged = previousServerAgents.length !== action.agents.length;
      const incomingAgents = action.agents.map((agent) => {
        const normalized = normalizeAgent(agent, action.serverId, action.serverName, action.serverUrl);
        const previous = previousByKey.get(normalized.key);
        if (previous && agentsEqual(previous, normalized)) {
          return previous;
        }
        agentsChanged = true;
        return normalized;
      });
      const nextAgents = reconcileServerAgents(state.agents, action.serverId, incomingAgents);
      if (!agentsChanged && nextAgents.some((agent, index) => agent !== state.agents[index])) {
        agentsChanged = true;
      }
      const hydratedServers = markServerHydrated(state.hydratedServers, action.serverId);
      const agentSessionListGenerationByServer = stampAgentSessionListGeneration(
        state,
        action.serverId,
      );

      if (
        !agentsChanged &&
        hydratedServers === state.hydratedServers &&
        agentSessionListGenerationByServer === state.agentSessionListGenerationByServer
      ) {
        return state;
      }

      return {
        ...state,
        agents: nextAgents,
        hydratedServers,
        agentSessionListGenerationByServer,
      };
    }
    case 'UPSERT_AGENT': {
      const nextAgent = normalizeAgent(action.agent, action.serverId, action.serverName, action.serverUrl);
      const existingIndex = state.agents.findIndex(agent => agent.key === nextAgent.key);
      const existing = existingIndex >= 0 ? state.agents[existingIndex] : undefined;
      const hydratedServers = markServerHydrated(state.hydratedServers, action.serverId);
      if (
        existing &&
        agentsEqual(existing, nextAgent) &&
        hydratedServers === state.hydratedServers
      ) {
        return state;
      }
      return {
        ...state,
        agents: existing
          ? state.agents.map(agent => (agent.key === nextAgent.key ? nextAgent : agent))
          : [...state.agents, nextAgent],
        hydratedServers,
      };
    }
    case 'REMOVE_AGENT': {
      const targetKey = makeSessionKey(action.serverId, action.agent_id);
      if (!state.agents.some(agent => agent.key === targetKey)) {
        return state;
      }
      return {
        ...state,
        agents: state.agents.filter(agent => agent.key !== targetKey),
      };
    }
    case 'SET_SERVER_CONNECTION_STATE':
      if (state.serverConnections[action.serverId] === action.connectionState) {
        return state;
      }
      {
        const previous = state.serverConnections[action.serverId];
        const connectionGenerationByServer = bumpServerConnectionGeneration(
          state.connectionGenerationByServer,
          action.serverId,
          previous,
          action.connectionState,
        );
        return {
          ...state,
          serverConnections: {
            ...state.serverConnections,
            [action.serverId]: action.connectionState,
          },
          connectionGenerationByServer,
        };
      }
    case 'SET_SERVER_CONNECTION_ISSUE':
      if (connectionIssuesEqual(state.serverConnectionIssues[action.serverId] ?? null, action.issue)) {
        return state;
      }
      return {
        ...state,
        serverConnectionIssues: {
          ...state.serverConnectionIssues,
          [action.serverId]: action.issue,
        },
      };
    case 'SET_SERVER_LATENCY':
      if (serverLatencySamplesEqual(state.serverLatencyById[action.serverId], action.sample)) {
        return state;
      }
      return {
        ...state,
        serverLatencyById: {
          ...state.serverLatencyById,
          [action.serverId]: action.sample,
        },
      };
    case 'REMOVE_SERVER':
      if (
        !state.agents.some(agent => agent.serverId === action.serverId) &&
        !(action.serverId in state.serverConnections) &&
        !(action.serverId in state.serverConnectionIssues) &&
        !(action.serverId in state.serverLatencyById) &&
        !(action.serverId in state.hydratedServers) &&
        !(action.serverId in state.connectionGenerationByServer) &&
        !(action.serverId in state.agentSessionListGenerationByServer)
      ) {
        return state;
      }
      return {
        ...state,
        agents: state.agents.filter(agent => agent.serverId !== action.serverId),
        serverConnections: Object.fromEntries(
          Object.entries(state.serverConnections).filter(([serverId]) => serverId !== action.serverId),
        ),
        serverConnectionIssues: Object.fromEntries(
          Object.entries(state.serverConnectionIssues).filter(([serverId]) => serverId !== action.serverId),
        ),
        serverLatencyById: Object.fromEntries(
          Object.entries(state.serverLatencyById).filter(([serverId]) => serverId !== action.serverId),
        ),
        hydratedServers: Object.fromEntries(
          Object.entries(state.hydratedServers).filter(([serverId]) => serverId !== action.serverId),
        ),
        connectionGenerationByServer: Object.fromEntries(
          Object.entries(state.connectionGenerationByServer).filter(
            ([serverId]) => serverId !== action.serverId,
          ),
        ),
        agentSessionListGenerationByServer: Object.fromEntries(
          Object.entries(state.agentSessionListGenerationByServer).filter(
            ([serverId]) => serverId !== action.serverId,
          ),
        ),
      };
    default:
      return state;
  }
}

export function reconcileServerAgents(
  currentAgents: Agent[],
  serverId: string,
  incomingAgents: Agent[],
): Agent[] {
  const incomingByKey = new Map(incomingAgents.map(agent => [agent.key, agent]));
  const knownKeys = new Set(
    currentAgents
      .filter(agent => agent.serverId === serverId)
      .map(agent => agent.key),
  );
  const next = currentAgents.flatMap(agent => {
    if (agent.serverId !== serverId) return [agent];
    const replacement = incomingByKey.get(agent.key);
    return replacement ? [replacement] : [];
  });

  for (const agent of incomingAgents) {
    if (!knownKeys.has(agent.key)) next.push(agent);
  }
  return next;
}

export function countAgentsByServer(
  agents: readonly Pick<Agent, 'serverId'>[],
): Record<string, number> {
  const counts: Record<string, number> = {};
  for (const agent of agents) {
    counts[agent.serverId] = (counts[agent.serverId] ?? 0) + 1;
  }
  return counts;
}

function markServerHydrated(
  hydratedServers: State['hydratedServers'],
  serverId: string,
): State['hydratedServers'] {
  if (hydratedServers[serverId]) {
    return hydratedServers;
  }
  return {
    ...hydratedServers,
    [serverId]: true,
  };
}

/**
 * Full agent_session_list is the transport proof that retained agents were
 * replaced/confirmed for the current WebSocket connection generation.
 * Incremental UPSERT_AGENT must not stamp this.
 */
function stampAgentSessionListGeneration(
  state: State,
  serverId: string,
): State['agentSessionListGenerationByServer'] {
  return stampAgentSessionListGenerationForServer({
    connectionState: state.serverConnections[serverId],
    connectionGeneration: state.connectionGenerationByServer[serverId] ?? 0,
    agentSessionListGenerationByServer: state.agentSessionListGenerationByServer,
    serverId,
  });
}

/** True when a full agent_session_list arrived for the current connected generation. */
export function isAgentSessionListFreshForConnection(
  state: Pick<
    State,
    | 'serverConnections'
    | 'connectionGenerationByServer'
    | 'agentSessionListGenerationByServer'
  >,
  serverId: string,
): boolean {
  return isAgentSessionListFreshForConnectionInput({
    connectionState: state.serverConnections[serverId],
    connectionGeneration: state.connectionGenerationByServer[serverId] ?? 0,
    agentSessionListGeneration:
      state.agentSessionListGenerationByServer[serverId] ?? 0,
  });
}

function agentsEqual(left: Agent, right: Agent): boolean {
  return (
    left === right ||
    (
      left.key === right.key &&
      left.id === right.id &&
      left.serverId === right.serverId &&
      left.serverName === right.serverName &&
      left.serverUrl === right.serverUrl &&
      left.name === right.name &&
      left.status === right.status &&
      left.project === right.project &&
      left.cwd === right.cwd &&
      left.command === right.command &&
      left.summary === right.summary &&
      left.phase === right.phase &&
      left.attention === right.attention &&
      left.task_class === right.task_class &&
      left.event_kind === right.event_kind &&
      left.details_json === right.details_json &&
      left.needs_attention === right.needs_attention &&
      stringArraysEqual(left.last_output_lines, right.last_output_lines) &&
      left.started_at === right.started_at &&
      left.updated_at === right.updated_at &&
      left.process_id === right.process_id &&
      left.delegated === right.delegated &&
      left.capabilities?.structured_events ===
        right.capabilities?.structured_events &&
      left.capabilities?.model_profile_managed ===
        right.capabilities?.model_profile_managed &&
      left.capabilities?.model_profile_active_switch ===
        right.capabilities?.model_profile_active_switch
    )
  );
}

function connectionIssuesEqual(
  left: ConnectionIssue | null | undefined,
  right: ConnectionIssue | null | undefined,
): boolean {
  if (left === right) {
    return true;
  }
  if (!left || !right) {
    return false;
  }
  return (
    left.code === right.code &&
    left.title === right.title &&
    left.detail === right.detail &&
    left.hint === right.hint &&
    left.checkedAt === right.checkedAt &&
    left.httpStatus === right.httpStatus
  );
}

function serverLatencySamplesEqual(
  left: ServerLatencySample | undefined,
  right: ServerLatencySample | undefined,
): boolean {
  if (left === right) {
    return true;
  }
  if (!left || !right) {
    return false;
  }
  return left.latencyMs === right.latencyMs && left.measuredAt === right.measuredAt;
}

function stringArraysEqual(left: string[], right: string[]): boolean {
  if (left === right) {
    return true;
  }
  if (left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) {
      return false;
    }
  }
  return true;
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
    phase: typeof agent.phase === 'string' ? agent.phase : undefined,
    attention: typeof agent.attention === 'string' ? agent.attention : undefined,
    task_class: typeof agent.task_class === 'string' ? agent.task_class : undefined,
    event_kind: typeof agent.event_kind === 'string' ? agent.event_kind : undefined,
    details_json: typeof agent.details_json === 'string' ? agent.details_json : undefined,
    needs_attention: agent.needs_attention === true,
    last_output_lines: Array.isArray(agent.last_output_lines) ? agent.last_output_lines : [],
    started_at: agent.started_at === undefined ? undefined : normalizeTimestamp(agent.started_at),
    updated_at: normalizeTimestamp(agent.updated_at),
    process_id: typeof agent.process_id === 'number' && Number.isFinite(agent.process_id)
      ? agent.process_id
      : undefined,
    delegated: agent.delegated === true,
    capabilities: normalizeAgentCapabilities(agent.capabilities),
  };
}

function normalizeAgentCapabilities(
  capabilities: RawAgent['capabilities'],
): AgentCapabilities | undefined {
  return normalizeAgentSessionCapabilities(capabilities);
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

const AgentStateContext = createContext<State | null>(null);
const AgentDispatchContext = createContext<React.Dispatch<Action> | null>(null);
const AgentListContext = createContext<State['agents'] | null>(null);
const AgentServerConnectionsContext = createContext<
  State['serverConnections'] | null
>(null);
const AgentServerSummaryContext = createContext<{
  serverConnections: State['serverConnections'];
  serverConnectionIssues: State['serverConnectionIssues'];
  serverLatencyById: State['serverLatencyById'];
  hydratedServers: State['hydratedServers'];
  dispatch: React.Dispatch<Action>;
} | null>(null);

export function AgentProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(agentReducer, initialAgentState);
  const serverSummaryValue = React.useMemo(
    () => ({
      serverConnections: state.serverConnections,
      serverConnectionIssues: state.serverConnectionIssues,
      serverLatencyById: state.serverLatencyById,
      hydratedServers: state.hydratedServers,
      dispatch,
    }),
    [
      state.hydratedServers,
      state.serverConnectionIssues,
      state.serverConnections,
      state.serverLatencyById,
    ],
  );
  return (
    <AgentDispatchContext.Provider value={dispatch}>
      <AgentStateContext.Provider value={state}>
        <AgentListContext.Provider value={state.agents}>
          <AgentServerConnectionsContext.Provider
            value={state.serverConnections}
          >
            <AgentServerSummaryContext.Provider value={serverSummaryValue}>
              {children}
            </AgentServerSummaryContext.Provider>
          </AgentServerConnectionsContext.Provider>
        </AgentListContext.Provider>
      </AgentStateContext.Provider>
    </AgentDispatchContext.Provider>
  );
}

export function useAgents() {
  const state = useContext(AgentStateContext);
  const dispatch = useContext(AgentDispatchContext);
  if (!state || !dispatch) {
    throw new Error('useAgents must be used within AgentProvider');
  }
  return { state, dispatch };
}

export function useAgentDispatch() {
  const dispatch = useContext(AgentDispatchContext);
  if (!dispatch) {
    throw new Error('useAgentDispatch must be used within AgentProvider');
  }
  return dispatch;
}

export function useAgentList() {
  const agents = useContext(AgentListContext);
  if (!agents) {
    throw new Error('useAgentList must be used within AgentProvider');
  }
  return agents;
}

export function useAgentServerConnections() {
  const serverConnections = useContext(AgentServerConnectionsContext);
  if (!serverConnections) {
    throw new Error(
      'useAgentServerConnections must be used within AgentProvider',
    );
  }
  return serverConnections;
}

export function useAgentServerSummary() {
  const ctx = useContext(AgentServerSummaryContext);
  if (!ctx) {
    throw new Error('useAgentServerSummary must be used within AgentProvider');
  }
  return ctx;
}
