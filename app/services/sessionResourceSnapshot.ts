export type SessionResourceHostPressure = "ok" | "pressure";

export type SessionResourceBackend =
  | "cgroup_pool"
  | "portable_supervisor"
  | string;

export interface SessionResourceSession {
  name?: string;
  executor?: string;
  command?: string;
  status?: string;
  phase?: string;
  started_at?: string;
  cwd?: string;
  delegated?: boolean;
  managed?: boolean;
  backend?: SessionResourceBackend;
  memory_current_bytes?: number;
  memory_peak_bytes?: number;
  tasks_current?: number;
}

export interface SessionResourcePool {
  backend?: SessionResourceBackend;
  memory_current_bytes?: number;
  memory_high_bytes?: number;
  memory_max_bytes?: number;
}

export interface SessionResourceHost {
  available_bytes?: number;
  pressure?: SessionResourceHostPressure | string;
}

export interface SessionResourceSnapshot {
  request_id?: string;
  agent_id: string;
  session: SessionResourceSession;
  pool?: SessionResourcePool | null;
  host?: SessionResourceHost | null;
}

export function normalizeSessionResourceSnapshot(
  payload: unknown,
): SessionResourceSnapshot | null {
  if (!payload || typeof payload !== "object") {
    return null;
  }
  const source = payload as Record<string, unknown>;
  const agentId =
    typeof source.agent_id === "string" ? source.agent_id.trim() : "";
  if (!agentId) {
    return null;
  }
  return {
    request_id:
      typeof source.request_id === "string" ? source.request_id : undefined,
    agent_id: agentId,
    session: normalizeSession(source.session),
    pool: normalizePool(source.pool),
    host: normalizeHost(source.host),
  };
}

/**
 * Production freshness for Session resource snapshots:
 * unique request_id (websocket), serverId (socket wrapper), agent_id, and the
 * hook request epoch invalidated on disconnect / session / server / close.
 * There is no authoritative generation to compare against.
 */
export function acceptSessionResourceSnapshotResponse({
  requestSeq,
  currentSeq,
  snapshotAgentId,
  expectedAgentId,
}: {
  requestSeq: number;
  currentSeq: number;
  snapshotAgentId: string;
  expectedAgentId: string;
}): boolean {
  return (
    requestSeq === currentSeq &&
    expectedAgentId !== "" &&
    snapshotAgentId === expectedAgentId
  );
}

export function formatByteSize(bytes: number | undefined): string | null {
  if (typeof bytes !== "number" || !Number.isFinite(bytes) || bytes < 0) {
    return null;
  }
  const units = ["B", "KiB", "MiB", "GiB", "TiB"] as const;
  let value = bytes;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }
  const digits = value >= 100 || unitIndex === 0 ? 0 : value >= 10 ? 1 : 2;
  return `${value.toFixed(digits)} ${units[unitIndex]}`;
}

export function formatExactBytesLabel(bytes: number | undefined): string | null {
  if (typeof bytes !== "number" || !Number.isFinite(bytes) || bytes < 0) {
    return null;
  }
  return `${Math.round(bytes)} bytes`;
}

export function formatUptime(startedAt: string | undefined, nowMs = Date.now()): string | null {
  if (!startedAt) {
    return null;
  }
  const started = Date.parse(startedAt);
  if (!Number.isFinite(started) || started <= 0 || started > nowMs) {
    return null;
  }
  const totalSeconds = Math.floor((nowMs - started) / 1000);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }
  if (minutes > 0) {
    return `${minutes}m ${seconds}s`;
  }
  return `${seconds}s`;
}

function normalizeSession(raw: unknown): SessionResourceSession {
  const source = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    name: optionalString(source.name),
    executor: optionalString(source.executor),
    command: optionalString(source.command),
    status: optionalString(source.status),
    phase: optionalString(source.phase),
    started_at: optionalString(source.started_at),
    cwd: optionalString(source.cwd),
    delegated: optionalBoolean(source.delegated),
    managed: optionalBoolean(source.managed),
    backend: optionalString(source.backend),
    memory_current_bytes: optionalNonNegativeNumber(source.memory_current_bytes),
    memory_peak_bytes: optionalNonNegativeNumber(source.memory_peak_bytes),
    tasks_current: optionalNonNegativeNumber(source.tasks_current),
  };
}

function normalizePool(raw: unknown): SessionResourcePool | null {
  if (!raw || typeof raw !== "object") {
    return null;
  }
  const source = raw as Record<string, unknown>;
  const pool: SessionResourcePool = {
    backend: optionalString(source.backend),
    memory_current_bytes: optionalNonNegativeNumber(source.memory_current_bytes),
    memory_high_bytes: optionalNonNegativeNumber(source.memory_high_bytes),
    memory_max_bytes: optionalNonNegativeNumber(source.memory_max_bytes),
  };
  if (
    pool.backend == null &&
    pool.memory_current_bytes == null &&
    pool.memory_high_bytes == null &&
    pool.memory_max_bytes == null
  ) {
    return null;
  }
  return pool;
}

function normalizeHost(raw: unknown): SessionResourceHost | null {
  if (!raw || typeof raw !== "object") {
    return null;
  }
  const source = raw as Record<string, unknown>;
  const host: SessionResourceHost = {
    available_bytes: optionalNonNegativeNumber(source.available_bytes),
    pressure: optionalString(source.pressure),
  };
  if (host.available_bytes == null && host.pressure == null) {
    return null;
  }
  return host;
}

function optionalString(value: unknown): string | undefined {
  if (typeof value !== "string") {
    return undefined;
  }
  const trimmed = value.trim();
  return trimmed || undefined;
}

function optionalBoolean(value: unknown): boolean | undefined {
  return typeof value === "boolean" ? value : undefined;
}

function optionalNonNegativeNumber(value: unknown): number | undefined {
  if (typeof value !== "number" || !Number.isFinite(value) || value < 0) {
    return undefined;
  }
  return value;
}
