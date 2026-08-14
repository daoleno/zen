import type { Agent, ConnectionState } from "../store/agents";

/**
 * Telegram-style multi-selection over the Sessions list.
 *
 * Selection is keyed by the app's canonical stable Session key
 * (`makeSessionKey(serverId, agentId)`), never by row index, display name or
 * order. That is what lets selection survive list reorder, section moves and
 * live updates; it is pruned only when the authoritative Session row
 * disappears from the daemon-backed store.
 */
export type SessionSelection = ReadonlySet<string>;

export const EMPTY_SESSION_SELECTION: SessionSelection = new Set<string>();

/** Add or remove one Session key. Always returns a new Set. */
export function toggleSessionSelection(
  selection: SessionSelection,
  sessionKey: string,
): SessionSelection {
  const next = new Set(selection);
  if (next.has(sessionKey)) {
    next.delete(sessionKey);
  } else {
    next.add(sessionKey);
  }
  return next;
}

/** Add one Session key. Returns the same set when already selected. */
export function addSessionToSelection(
  selection: SessionSelection,
  sessionKey: string,
): SessionSelection {
  if (selection.has(sessionKey)) {
    return selection;
  }
  const next = new Set(selection);
  next.add(sessionKey);
  return next;
}

/** Remove Session keys. Returns the same set when none were selected. */
export function removeSessionsFromSelection(
  selection: SessionSelection,
  sessionKeys: Iterable<string>,
): SessionSelection {
  const next = new Set(selection);
  let changed = false;
  for (const sessionKey of sessionKeys) {
    if (next.delete(sessionKey)) {
      changed = true;
    }
  }
  return changed ? next : selection;
}

/**
 * Keep only keys whose authoritative row still exists. Returns the same set
 * reference when nothing was pruned so React state updates can bail out.
 */
export function pruneSessionSelection(
  selection: SessionSelection,
  authoritativeSessionKeys: Iterable<string>,
): SessionSelection {
  if (selection.size === 0) {
    return selection;
  }
  const known = new Set(authoritativeSessionKeys);
  const pruned = new Set<string>();
  for (const sessionKey of selection) {
    if (known.has(sessionKey)) {
      pruned.add(sessionKey);
    }
  }
  return pruned.size === selection.size ? selection : pruned;
}

export function countSessionSelection(selection: SessionSelection): number {
  return selection.size;
}

export function selectionCountLabel(count: number): string {
  return count === 1 ? "1 selected" : `${count} selected`;
}

/** Truthful, human-readable reason a row cannot be terminated (or null). */
export interface SessionTerminationEligibility {
  eligible: boolean;
  reason: string | null;
}

/**
 * Termination eligibility follows the daemon contract and the existing
 * single-Session terminate rule: every Session in the authoritative list is
 * terminable through `kill_agent` (kill is idempotent for already-gone
 * Sessions, and Brain/user-owned Sessions are regular tmux Sessions on the
 * daemon), but only while its daemon connection is established. Offline rows
 * stay visible but are excluded from selection with a truthful reason.
 */
export function sessionTerminationEligibility(
  connectionState: ConnectionState | undefined,
): SessionTerminationEligibility {
  if (connectionState === "connected") {
    return { eligible: true, reason: null };
  }
  return {
    eligible: false,
    reason: "Daemon is not connected",
  };
}

export function isSessionTerminable(
  agent: Pick<Agent, "serverId">,
  serverConnections: Record<string, ConnectionState>,
): boolean {
  return sessionTerminationEligibility(serverConnections[agent.serverId])
    .eligible;
}

/** Number of distinct daemons spanned by a selection (for confirmation copy). */
export function countSelectionServers(
  agents: readonly Pick<Agent, "serverId">[],
): number {
  return new Set(agents.map((agent) => agent.serverId)).size;
}
