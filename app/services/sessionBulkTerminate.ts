/**
 * Batch Session termination against the daemon's authoritative terminate
 * lifecycle (`kill_agent` → route-aware teardown → watcher removal).
 *
 * Contract audited from daemon/server/server.go and daemon/modelprofiles:
 * - Command: `kill_agent` with `agent_id` and optional `request_id`.
 * - Failure: daemon replies `{type:"error", code, message, request_id, ...}`.
 * - Success: no direct reply. The authoritative acknowledgement is the Session
 *   leaving the daemon list: either the `agent_session_archived` event or
 *   absence from the next full `agent_session_list` snapshot (heartbeat and
 *   refresh broadcasts replace the whole set).
 *
 * The batch never deletes locally and never invents state: entries are a
 * point-in-time snapshot of stable Session IDs; every entry settles from
 * daemon-owned evidence or a retryable timeout, and a settled entry never
 * changes again (first-wins), so concurrent disappearance and duplicate
 * events cannot corrupt the outcome.
 */
export type SessionTerminationStatus = "pending" | "succeeded" | "failed";

export interface SessionTerminationEntry {
  /** Canonical stable key: makeSessionKey(serverId, agentId). */
  sessionKey: string;
  serverId: string;
  agentId: string;
  status: SessionTerminationStatus;
  /** Truthful failure detail for the retryable summary. */
  error?: string;
}

export interface SessionTerminationSummary {
  total: number;
  succeeded: number;
  failed: number;
  pending: number;
  running: boolean;
  /** Only settled failures, in submission order. */
  failedEntries: SessionTerminationEntry[];
}

/**
 * Minimal structural transport; MultiServerWebSocketClient satisfies it.
 * Kept narrow so the batch is fully testable with a fake.
 */
export interface SessionTerminationTransport {
  on(type: string, handler: (data: any) => void): void;
  off(type: string, handler: (data: any) => void): void;
  killAgent(serverId: string, agentId: string, requestId?: string): void;
}

export interface SessionTerminationBatchOptions {
  transport: SessionTerminationTransport;
  /** Snapshot of stable Session IDs to terminate (must be non-empty). */
  entries: SessionTerminationEntry[];
  /** Called on every settle with the current summary; final call has running=false. */
  onSettled(summary: SessionTerminationSummary): void;
  /** Per-entry acknowledgement timeout; default 20s. */
  timeoutMs?: number;
}

export interface SessionTerminationTarget {
  sessionKey: string;
  serverId: string;
  agentId: string;
}

/** Build the pending entry snapshot for a set of stable Session targets. */
export function createSessionTerminationEntries(
  targets: readonly SessionTerminationTarget[],
): SessionTerminationEntry[] {
  return targets.map((target) => ({
    sessionKey: target.sessionKey,
    serverId: target.serverId,
    agentId: target.agentId,
    status: "pending" as const,
  }));
}

export function summarizeSessionTermination(
  entries: readonly SessionTerminationEntry[],
): SessionTerminationSummary {
  let succeeded = 0;
  let failed = 0;
  let pending = 0;
  const failedEntries: SessionTerminationEntry[] = [];
  for (const entry of entries) {
    switch (entry.status) {
      case "succeeded":
        succeeded += 1;
        break;
      case "failed":
        failed += 1;
        failedEntries.push(entry);
        break;
      default:
        pending += 1;
        break;
    }
  }
  return {
    total: entries.length,
    succeeded,
    failed,
    pending,
    running: pending > 0,
    failedEntries,
  };
}

export const SESSION_TERMINATION_TIMEOUT_MS = 20000;
export const SESSION_TERMINATION_TIMEOUT_MESSAGE =
  "The daemon did not acknowledge termination in time. Retry to terminate this session.";
export const SESSION_TERMINATION_OFFLINE_MESSAGE =
  "The daemon is not connected. Reconnect and retry to terminate this session.";

function newBatchRequestId(): string {
  return `kill_${Date.now().toString(36)}_${Math.random()
    .toString(36)
    .slice(2, 10)}`;
}

export function sessionTerminationConfirmMessage(
  count: number,
  serverCount: number,
): string {
  const target = count === 1 ? "This session" : `These ${count} sessions`;
  const where = serverCount > 1 ? ` across ${serverCount} daemons` : "";
  return `${target} will be terminated${where}. Sessions leave the list once the daemon confirms termination.`;
}

export function sessionTerminationSummaryMessage(
  summary: SessionTerminationSummary,
  failedTitles: readonly string[],
): string {
  const failedCount = summary.failed;
  if (failedCount === 0) {
    return "";
  }
  const failedLabel =
    failedCount === 1 ? "1 session" : `${failedCount} sessions`;
  const lead =
    summary.succeeded === 0
      ? `Could not terminate ${failedLabel}.`
      : `${summary.succeeded} of ${summary.total} terminated; ${failedLabel} failed.`;
  const names = failedTitles.filter(Boolean);
  const detail =
    names.length > 0
      ? `\n\n${names.join(", ")}`
      : "";
  return `${lead}${detail}\n\nThese sessions remain selected. Retry to terminate them.`;
}

function archivedAgentId(payload: any): string | null {
  const id = payload?.agent_session?.id;
  return typeof id === "string" && id ? id : null;
}

function listedAgentIds(payload: any): Set<string> {
  const ids = new Set<string>();
  const sessions = Array.isArray(payload?.agent_sessions)
    ? payload.agent_sessions
    : [];
  for (const session of sessions) {
    if (session && typeof session.id === "string" && session.id) {
      ids.add(session.id);
    }
  }
  return ids;
}

/**
 * Runs one confirmed batch. `start()` submits exactly once (a second call is
 * a no-op), subscribes to daemon events, and settles each entry from
 * authoritative evidence:
 * - `error` matching this batch's request_id → failed (daemon message kept).
 * - `agent_session_archived` for the Session → succeeded.
 * - absence from a full `agent_session_list` snapshot → succeeded.
 * - per-entry timeout → failed (retryable).
 * - `settleDisappeared` (called by the UI when the authoritative row vanished
 *   while the batch was running) → succeeded, treated as already settled.
 */
export class SessionTerminationBatch {
  private readonly entries: SessionTerminationEntry[];
  private readonly transport: SessionTerminationTransport;
  private readonly onSettled: (summary: SessionTerminationSummary) => void;
  private readonly timeoutMs: number;
  private readonly timers = new Map<string, ReturnType<typeof setTimeout>>();
  private readonly requestIdBySessionKey = new Map<string, string>();
  private readonly handlers: Array<{
    type: string;
    handler: (data: any) => void;
  }> = [];
  private started = false;
  private disposed = false;

  constructor(options: SessionTerminationBatchOptions) {
    this.entries = createSessionTerminationEntries(options.entries);
    this.transport = options.transport;
    this.onSettled = options.onSettled;
    this.timeoutMs = options.timeoutMs ?? SESSION_TERMINATION_TIMEOUT_MS;
  }

  get summary(): SessionTerminationSummary {
    return summarizeSessionTermination(this.entries);
  }

  get isRunning(): boolean {
    return this.started && !this.disposed && this.summary.pending > 0;
  }

  /**
   * Submit the batch. Returns false (without submitting) when already
   * started, preventing duplicate submissions while a batch is running.
   */
  start(): boolean {
    if (this.started) {
      return false;
    }
    this.started = true;
    this.subscribe();

    for (const entry of this.entries) {
      if (entry.status !== "pending") {
        continue;
      }
      const requestId = newBatchRequestId();
      this.requestIdBySessionKey.set(entry.sessionKey, requestId);
      try {
        this.transport.killAgent(entry.serverId, entry.agentId, requestId);
      } catch {
        this.settle(
          entry.sessionKey,
          "failed",
          SESSION_TERMINATION_OFFLINE_MESSAGE,
        );
        continue;
      }
      this.timers.set(
        entry.sessionKey,
        setTimeout(() => {
          this.timers.delete(entry.sessionKey);
          this.settle(
            entry.sessionKey,
            "failed",
            SESSION_TERMINATION_TIMEOUT_MESSAGE,
          );
        }, this.timeoutMs),
      );
    }
    this.emitSummary();
    return true;
  }

  /** Treat a Session that authoritatively disappeared as already settled. */
  settleDisappeared(sessionKey: string): void {
    this.settle(sessionKey, "succeeded");
  }

  dispose(): void {
    if (this.disposed) {
      return;
    }
    this.disposed = true;
    for (const timer of this.timers.values()) {
      clearTimeout(timer);
    }
    this.timers.clear();
    for (const { type, handler } of this.handlers) {
      this.transport.off(type, handler);
    }
    this.handlers.length = 0;
  }

  private settle(
    sessionKey: string,
    status: SessionTerminationStatus,
    error?: string,
  ): void {
    if (this.disposed) {
      return;
    }
    const entry = this.entries.find(
      (candidate) =>
        candidate.sessionKey === sessionKey &&
        candidate.status === "pending",
    );
    if (!entry) {
      // Already settled: first-wins, so concurrent disappearance after a
      // failure reply (or vice versa) can never flip an outcome.
      return;
    }
    entry.status = status;
    if (error) {
      entry.error = error;
    }
    const timer = this.timers.get(sessionKey);
    if (timer) {
      clearTimeout(timer);
      this.timers.delete(sessionKey);
    }
    this.emitSummary();
  }

  private emitSummary(): void {
    this.onSettled(this.summary);
  }

  private subscribe(): void {
    const onError = (data: any) => {
      if (data?.serverId == null || typeof data.request_id !== "string") {
        return;
      }
      for (const [sessionKey, requestId] of this.requestIdBySessionKey) {
        if (
          requestId !== data.request_id ||
          data.serverId !== this.entryFor(sessionKey)?.serverId
        ) {
          continue;
        }
        const message =
          typeof data.message === "string" && data.message
            ? data.message
            : SESSION_TERMINATION_TIMEOUT_MESSAGE;
        this.settle(sessionKey, "failed", message);
      }
    };
    const onArchived = (data: any) => {
      const agentId = archivedAgentId(data);
      if (agentId == null || data?.serverId == null) {
        return;
      }
      for (const entry of this.entries) {
        if (
          entry.status === "pending" &&
          entry.serverId === data.serverId &&
          entry.agentId === agentId
        ) {
          this.settle(entry.sessionKey, "succeeded");
        }
      }
    };
    const onList = (data: any) => {
      const serverId = data?.serverId;
      if (typeof serverId !== "string") {
        return;
      }
      const present = listedAgentIds(data);
      for (const entry of this.entries) {
        if (
          entry.status === "pending" &&
          entry.serverId === serverId &&
          !present.has(entry.agentId)
        ) {
          this.settle(entry.sessionKey, "succeeded");
        }
      }
    };
    const handlers: Array<{
      type: string;
      handler: (data: any) => void;
    }> = [
      { type: "error", handler: onError },
      { type: "agent_session_archived", handler: onArchived },
      { type: "agent_session_list", handler: onList },
    ];
    for (const { type, handler } of handlers) {
      this.transport.on(type, handler);
    }
    this.handlers.push(...handlers);
  }

  private entryFor(sessionKey: string): SessionTerminationEntry | undefined {
    return this.entries.find((entry) => entry.sessionKey === sessionKey);
  }
}
