import React, { createContext, useContext, useReducer, ReactNode } from 'react';
import { WorkerStatus } from '../constants/tokens';
import type { ConnectionIssue } from '../services/connectionIssue';
import type { ServerLatencySample } from '../services/serverLatency';
import { makeSessionKey } from '../services/sessionKeys';
import {
  bumpServerConnectionGeneration,
  isWorkerSessionListFreshForConnection as isWorkerSessionListFreshForConnectionInput,
  stampWorkerSessionListGeneration as stampWorkerSessionListGenerationForServer,
} from '../services/workerSessionListTransport';
import {
  normalizeWorkerSessionCapabilities,
  type WorkerSessionCapabilities,
} from '../services/providers/sessionCapabilities';

export type WorkerCapabilities = WorkerSessionCapabilities;

export interface Worker {
  key: string;
  id: string;
  serverId: string;
  serverName: string;
  serverUrl: string;
  name: string;
  status: WorkerStatus;
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
  updated_at?: number;
  process_id?: number;
  delegated?: boolean;
  capabilities?: WorkerCapabilities;
}

export type ConnectionState = 'offline' | 'connecting' | 'connected';

export interface State {
  workers: Worker[];
  serverConnections: Record<string, ConnectionState>;
  serverConnectionIssues: Record<string, ConnectionIssue | null>;
  serverLatencyById: Record<string, ServerLatencySample | undefined>;
  hydratedServers: Record<string, boolean>;
  /**
   * Transport-owned: increments when a server enters `connected`.
   * Used with workerSessionListGenerationByServer to prove a full
   * worker_session_list arrived for the current WebSocket generation.
   */
  connectionGenerationByServer: Record<string, number>;
  /** Set to connectionGeneration when UPSERT_SERVER_WORKERS arrives while connected. */
  workerSessionListGenerationByServer: Record<string, number>;
}

export type RawWorker = {
  id: string;
  name: string;
  status: WorkerStatus;
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
      type: 'UPSERT_SERVER_WORKERS';
      serverId: string;
      serverName: string;
      serverUrl: string;
      workers: RawWorker[];
    }
  | {
      type: 'UPSERT_WORKER';
      serverId: string;
      serverName: string;
      serverUrl: string;
      worker: RawWorker;
    }
  | { type: 'REMOVE_WORKER'; serverId: string; worker_id: string }
  | { type: 'SET_SERVER_CONNECTION_STATE'; serverId: string; connectionState: ConnectionState }
  | { type: 'SET_SERVER_CONNECTION_ISSUE'; serverId: string; issue: ConnectionIssue | null }
  | { type: 'SET_SERVER_LATENCY'; serverId: string; sample: ServerLatencySample }
  | { type: 'REMOVE_SERVER'; serverId: string };

export const initialWorkerState: State = {
  workers: [],
  serverConnections: {},
  serverConnectionIssues: {},
  serverLatencyById: {},
  hydratedServers: {},
  connectionGenerationByServer: {},
  workerSessionListGenerationByServer: {},
};

export function workerReducer(state: State, action: Action): State {
  switch (action.type) {
    case 'UPSERT_SERVER_WORKERS': {
      const previousServerWorkers = state.workers.filter(agent => agent.serverId === action.serverId);
      const previousByKey = new Map(previousServerWorkers.map(agent => [agent.key, agent]));
      let agentsChanged = previousServerWorkers.length !== action.workers.length;
      const incomingWorkers = action.workers.map((agent) => {
        const normalized = normalizeWorker(agent, action.serverId, action.serverName, action.serverUrl);
        const previous = previousByKey.get(normalized.key);
        if (previous && agentsEqual(previous, normalized)) {
          return previous;
        }
        agentsChanged = true;
        return normalized;
      });
      const nextWorkers = reconcileServerWorkers(state.workers, action.serverId, incomingWorkers);
      if (!agentsChanged && nextWorkers.some((agent, index) => agent !== state.workers[index])) {
        agentsChanged = true;
      }
      const hydratedServers = markServerHydrated(state.hydratedServers, action.serverId);
      const workerSessionListGenerationByServer = stampWorkerSessionListGeneration(
        state,
        action.serverId,
      );

      if (
        !agentsChanged &&
        hydratedServers === state.hydratedServers &&
        workerSessionListGenerationByServer === state.workerSessionListGenerationByServer
      ) {
        return state;
      }

      return {
        ...state,
        workers: nextWorkers,
        hydratedServers,
        workerSessionListGenerationByServer,
      };
    }
    case 'UPSERT_WORKER': {
      const nextWorker = normalizeWorker(action.worker, action.serverId, action.serverName, action.serverUrl);
      const existingIndex = state.workers.findIndex(agent => agent.key === nextWorker.key);
      const existing = existingIndex >= 0 ? state.workers[existingIndex] : undefined;
      const hydratedServers = markServerHydrated(state.hydratedServers, action.serverId);
      if (
        existing &&
        agentsEqual(existing, nextWorker) &&
        hydratedServers === state.hydratedServers
      ) {
        return state;
      }
      return {
        ...state,
        workers: existing
          ? state.workers.map(agent => (agent.key === nextWorker.key ? nextWorker : agent))
          : [...state.workers, nextWorker],
        hydratedServers,
      };
    }
    case 'REMOVE_WORKER': {
      const targetKey = makeSessionKey(action.serverId, action.worker_id);
      if (!state.workers.some(agent => agent.key === targetKey)) {
        return state;
      }
      return {
        ...state,
        workers: state.workers.filter(agent => agent.key !== targetKey),
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
        !state.workers.some(agent => agent.serverId === action.serverId) &&
        !(action.serverId in state.serverConnections) &&
        !(action.serverId in state.serverConnectionIssues) &&
        !(action.serverId in state.serverLatencyById) &&
        !(action.serverId in state.hydratedServers) &&
        !(action.serverId in state.connectionGenerationByServer) &&
        !(action.serverId in state.workerSessionListGenerationByServer)
      ) {
        return state;
      }
      return {
        ...state,
        workers: state.workers.filter(agent => agent.serverId !== action.serverId),
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
        workerSessionListGenerationByServer: Object.fromEntries(
          Object.entries(state.workerSessionListGenerationByServer).filter(
            ([serverId]) => serverId !== action.serverId,
          ),
        ),
      };
    default:
      return state;
  }
}

export function reconcileServerWorkers(
  currentWorkers: Worker[],
  serverId: string,
  incomingWorkers: Worker[],
): Worker[] {
  const incomingByKey = new Map(incomingWorkers.map(agent => [agent.key, agent]));
  const knownKeys = new Set(
    currentWorkers
      .filter(agent => agent.serverId === serverId)
      .map(agent => agent.key),
  );
  const next = currentWorkers.flatMap(agent => {
    if (agent.serverId !== serverId) return [agent];
    const replacement = incomingByKey.get(agent.key);
    return replacement ? [replacement] : [];
  });

  for (const agent of incomingWorkers) {
    if (!knownKeys.has(agent.key)) next.push(agent);
  }
  return next;
}

export function countWorkersByServer(
  agents: readonly Pick<Worker, 'serverId'>[],
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
 * Full worker_session_list is the transport proof that retained agents were
 * replaced/confirmed for the current WebSocket connection generation.
 * Incremental UPSERT_WORKER must not stamp this.
 */
function stampWorkerSessionListGeneration(
  state: State,
  serverId: string,
): State['workerSessionListGenerationByServer'] {
  return stampWorkerSessionListGenerationForServer({
    connectionState: state.serverConnections[serverId],
    connectionGeneration: state.connectionGenerationByServer[serverId] ?? 0,
    workerSessionListGenerationByServer: state.workerSessionListGenerationByServer,
    serverId,
  });
}

/** True when a full worker_session_list arrived for the current connected generation. */
export function isWorkerSessionListFreshForConnection(
  state: Pick<
    State,
    | 'serverConnections'
    | 'connectionGenerationByServer'
    | 'workerSessionListGenerationByServer'
  >,
  serverId: string,
): boolean {
  return isWorkerSessionListFreshForConnectionInput({
    connectionState: state.serverConnections[serverId],
    connectionGeneration: state.connectionGenerationByServer[serverId] ?? 0,
    workerSessionListGeneration:
      state.workerSessionListGenerationByServer[serverId] ?? 0,
  });
}

function agentsEqual(left: Worker, right: Worker): boolean {
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

function normalizeWorker(
  agent: RawWorker,
  serverId: string,
  serverName: string,
  serverUrl: string,
): Worker {
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
    started_at: normalizeTimestamp(agent.started_at),
    updated_at: normalizeTimestamp(agent.updated_at),
    process_id: typeof agent.process_id === 'number' && Number.isFinite(agent.process_id)
      ? agent.process_id
      : undefined,
    delegated: agent.delegated === true,
    capabilities: normalizeWorkerCapabilities(agent.capabilities),
  };
}

function normalizeWorkerCapabilities(
  capabilities: RawWorker['capabilities'],
): WorkerCapabilities | undefined {
  return normalizeWorkerSessionCapabilities(capabilities);
}

/**
 * Timestamps are seconds or milliseconds since the Unix epoch. Invalid or
 * missing values become undefined: rows must never silently fall back to the
 * current device time, which would collapse unrelated sessions to "now" for
 * both display and ordering.
 */
function normalizeTimestamp(
  value: RawWorker['updated_at'],
): number | undefined {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) {
    return value > 10_000_000_000 ? value : value * 1000;
  }

  if (typeof value === 'string') {
    const parsed = Date.parse(value);
    if (!Number.isNaN(parsed) && parsed > 0) return parsed;
  }

  if (value instanceof Date) {
    const parsed = value.getTime();
    if (Number.isFinite(parsed) && parsed > 0) return parsed;
  }

  return undefined;
}

const WorkerStateContext = createContext<State | null>(null);
const WorkerDispatchContext = createContext<React.Dispatch<Action> | null>(null);
const WorkerListContext = createContext<State['workers'] | null>(null);
const WorkerServerConnectionsContext = createContext<
  State['serverConnections'] | null
>(null);
const WorkerServerSummaryContext = createContext<{
  serverConnections: State['serverConnections'];
  serverConnectionIssues: State['serverConnectionIssues'];
  serverLatencyById: State['serverLatencyById'];
  hydratedServers: State['hydratedServers'];
  dispatch: React.Dispatch<Action>;
} | null>(null);

export function WorkerProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(workerReducer, initialWorkerState);
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
    <WorkerDispatchContext.Provider value={dispatch}>
      <WorkerStateContext.Provider value={state}>
        <WorkerListContext.Provider value={state.workers}>
          <WorkerServerConnectionsContext.Provider
            value={state.serverConnections}
          >
            <WorkerServerSummaryContext.Provider value={serverSummaryValue}>
              {children}
            </WorkerServerSummaryContext.Provider>
          </WorkerServerConnectionsContext.Provider>
        </WorkerListContext.Provider>
      </WorkerStateContext.Provider>
    </WorkerDispatchContext.Provider>
  );
}

export function useWorkers() {
  const state = useContext(WorkerStateContext);
  const dispatch = useContext(WorkerDispatchContext);
  if (!state || !dispatch) {
    throw new Error('useWorkers must be used within WorkerProvider');
  }
  return { state, dispatch };
}

export function useWorkerDispatch() {
  const dispatch = useContext(WorkerDispatchContext);
  if (!dispatch) {
    throw new Error('useWorkerDispatch must be used within WorkerProvider');
  }
  return dispatch;
}

export function useWorkerList() {
  const agents = useContext(WorkerListContext);
  if (!agents) {
    throw new Error('useWorkerList must be used within WorkerProvider');
  }
  return agents;
}

export function useWorkerServerConnections() {
  const serverConnections = useContext(WorkerServerConnectionsContext);
  if (!serverConnections) {
    throw new Error(
      'useWorkerServerConnections must be used within WorkerProvider',
    );
  }
  return serverConnections;
}

export function useWorkerServerSummary() {
  const ctx = useContext(WorkerServerSummaryContext);
  if (!ctx) {
    throw new Error('useWorkerServerSummary must be used within WorkerProvider');
  }
  return ctx;
}
