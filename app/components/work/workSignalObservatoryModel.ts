import type { BrainActiveWork } from "../../store/brain";
import type { AgentStatus } from "../../constants/tokens";
import type { SessionResourceSnapshot } from "../../services/sessionResourceSnapshot";

export type WorkSignalTone =
  | "active"
  | "waiting"
  | "attention"
  | "failed"
  | "complete"
  | "muted";

export type WorkSignalStage =
  | "owned"
  | "waiting"
  | "ready"
  | "completed"
  | "cancelled";

export type WorkSignalOwner = {
  sessionId: string;
  label: string;
  status: AgentStatus;
  delegated: boolean;
  needsAttention?: boolean;
};

export type WorkSignalItem = {
  id: string;
  title: string;
  stage: WorkSignalStage;
  tone: WorkSignalTone;
  signalLabel: string;
  detail?: string;
  ownerSessionId?: string;
  targetSessionId?: string;
  transitionKey: string;
  contradiction: boolean;
  accessibilityLabel: string;
};

export type WorkSignalObservatoryModel = {
  items: WorkSignalItem[];
  activeCount: number;
  ownerCount: number;
  waitingCount: number;
  attentionCount: number;
  failureCount: number;
  outcomeCount: number;
  allProgressAccountedFor: boolean;
  summaryLabel: string;
};

export type WorkSignalObservatoryProjection =
  | { state: "updating" }
  | { state: "ready"; model: WorkSignalObservatoryModel };

export type WorkResourcePresentation = {
  state: "idle" | "loading" | "steady" | "pressure" | "unavailable";
  label: string;
  level?: number;
};

export type WorkResourceRequestState =
  | { identity: string; status: "loading" }
  | {
      identity: string;
      status: "ready";
      snapshot: SessionResourceSnapshot;
    }
  | { identity: string; status: "failed" };

export type WorkResourceRequestProjection =
  | { identity: null; status: "idle"; snapshot: null }
  | { identity: string; status: "loading"; snapshot: null }
  | {
      identity: string;
      status: "ready";
      snapshot: SessionResourceSnapshot;
    }
  | { identity: string; status: "failed"; snapshot: null };

type Projection = Omit<
  WorkSignalItem,
  "id" | "title" | "transitionKey" | "accessibilityLabel"
>;

export function workResourceRequestIdentity(
  serverId: string | null,
  sessionId: string | null,
  connected: boolean,
  connectionGeneration: number,
): string | null {
  return serverId && sessionId && connected && connectionGeneration > 0
    ? `${serverId}\u0000${sessionId}\u0000${connectionGeneration}`
    : null;
}

export function buildWorkSignalObservatoryProjection({
  brainHydrated,
  agentListFresh,
  work,
  owners,
}: {
  brainHydrated: boolean;
  agentListFresh: boolean;
  work: readonly BrainActiveWork[];
  owners: readonly WorkSignalOwner[];
}): WorkSignalObservatoryProjection {
  if (!brainHydrated || !agentListFresh) {
    return { state: "updating" };
  }
  return {
    state: "ready",
    model: buildWorkSignalObservatoryModel(work, owners),
  };
}

export function reconcileStableWorkSignalItems(
  current: readonly WorkSignalItem[],
  incoming: readonly WorkSignalItem[],
): readonly WorkSignalItem[] {
  const incomingById = new Map(incoming.map((item) => [item.id, item]));
  const seen = new Set<string>();
  const next: WorkSignalItem[] = [];

  current.forEach((item) => {
    const replacement = incomingById.get(item.id);
    if (replacement) {
      seen.add(item.id);
      next.push(replacement);
    }
  });
  incoming.forEach((item) => {
    if (!seen.has(item.id)) {
      next.push(item);
    }
  });

  return next.length === current.length &&
    next.every((item, index) => item === current[index])
    ? current
    : next;
}

export function projectWorkResourceRequest(
  identity: string | null,
  request: WorkResourceRequestState | null,
): WorkResourceRequestProjection {
  if (!identity) {
    return { identity: null, status: "idle", snapshot: null };
  }
  if (!request || request.identity !== identity) {
    return { identity, status: "loading", snapshot: null };
  }
  if (request.status === "ready") {
    return request;
  }
  return { identity, status: request.status, snapshot: null };
}

export function buildWorkSignalObservatoryModel(
  work: readonly BrainActiveWork[],
  owners: readonly WorkSignalOwner[],
): WorkSignalObservatoryModel {
  const ownerById = new Map(owners.map((owner) => [owner.sessionId, owner]));
  const items = work.map((item) => projectWorkSignal(item, ownerById));
  const active = items.filter(
    (item) => item.stage !== "completed" && item.stage !== "cancelled",
  );
  const ownerCount = new Set(
    active
      .map((item) => (item.stage === "owned" ? item.ownerSessionId : undefined))
      .filter((sessionId): sessionId is string => Boolean(sessionId)),
  ).size;
  const waitingCount = active.filter((item) => item.stage === "waiting").length;
  const failureCount = items.filter((item) => item.tone === "failed").length;
  const attentionCount = items.filter(
    (item) => item.tone === "attention" || item.tone === "failed",
  ).length;
  const outcomeCount = items.length - active.length;

  return {
    items,
    activeCount: active.length,
    ownerCount,
    waitingCount,
    attentionCount,
    failureCount,
    outcomeCount,
    allProgressAccountedFor: items.every((item) => !item.contradiction),
    summaryLabel: summaryLabel(
      active.length,
      waitingCount,
      attentionCount,
      failureCount,
      outcomeCount,
    ),
  };
}

export function buildWorkResourcePresentation({
  activeCount,
  ownerCount,
  connected,
  loading,
  snapshot,
  failed,
}: {
  activeCount: number;
  ownerCount: number;
  connected: boolean;
  loading: boolean;
  snapshot: SessionResourceSnapshot | null;
  failed: boolean;
}): WorkResourcePresentation {
  if (activeCount === 0) return { state: "idle", label: "No active Sessions" };
  if (ownerCount === 0) return { state: "unavailable", label: "No active Session" };
  if (!connected) return { state: "unavailable", label: "Resources paused" };
  if (failed) return { state: "unavailable", label: "Resources unavailable" };
  if (!snapshot) {
    return loading
      ? { state: "loading", label: "Checking resources" }
      : { state: "unavailable", label: "Resources unavailable" };
  }

  const current = snapshot.pool?.memory_current_bytes;
  const limit =
    positiveNumber(snapshot.pool?.memory_high_bytes) ??
    positiveNumber(snapshot.pool?.memory_max_bytes);
  const ratio =
    typeof current === "number" && limit
      ? Math.max(0, current / limit)
      : undefined;
  const hostPressure = snapshot.host?.pressure?.trim().toLowerCase();
  const pressure = hostPressure === "pressure" || (ratio ?? 0) >= 1;
  if (ratio != null) {
    return {
      state: pressure ? "pressure" : "steady",
      label: `Memory ${Math.round(ratio * 100)}%`,
      level: Math.min(1, ratio),
    };
  }
  if (pressure) return { state: "pressure", label: "Resource pressure" };
  if (hostPressure === "ok") return { state: "steady", label: "Resources steady" };
  return { state: "unavailable", label: "Pressure unavailable" };
}

function projectWorkSignal(
  work: BrainActiveWork,
  ownerById: ReadonlyMap<string, WorkSignalOwner>,
): WorkSignalItem {
  const projection =
    work.status === "done" || work.status === "cancelled"
      ? projectTerminal(work)
      : work.progress_mode === "owned"
        ? projectOwned(work, ownerById)
        : work.progress_mode === "waiting"
          ? projectWaiting(work, ownerById)
          : work.progress_mode === "ready"
            ? projectReady(work)
            : problem("ready", "Progress unavailable");
  return withAccessibility({
    id: work.work_id,
    title: work.title,
    transitionKey: transitionKey(work),
    ...projection,
  });
}

function projectOwned(
  work: BrainActiveWork,
  ownerById: ReadonlyMap<string, WorkSignalOwner>,
): Projection {
  const sessionId = work.owner_session_id?.trim();
  if (!sessionId || work.owner_delegated !== true) {
    return problem("owned", "No Session assigned", sessionId);
  }
  const owner = ownerById.get(sessionId);
  if (!owner || !owner.delegated) {
    return problem("owned", "Session unavailable", sessionId);
  }

  const base = {
    stage: "owned" as const,
    signalLabel: owner.label,
    ownerSessionId: sessionId,
    targetSessionId: sessionId,
    contradiction: false,
  };
  if (owner.status === "failed") {
    return { ...base, tone: "failed", detail: "Session failed" };
  }
  if (owner.status === "blocked" || owner.needsAttention) {
    return { ...base, tone: "attention", detail: "Session needs review" };
  }
  if (owner.status === "done") {
    return {
      ...base,
      tone: "attention",
      detail: "Session finished · ready to continue",
    };
  }
  if (owner.status === "unknown") {
    return { ...base, tone: "attention", detail: "Session state unavailable" };
  }
  return { ...base, tone: "active", detail: "Active Session" };
}

function projectWaiting(
  work: BrainActiveWork,
  ownerById: ReadonlyMap<string, WorkSignalOwner>,
): Projection {
  if (!work.wake) return problem("waiting", "Waiting details unavailable");
  if (work.wake.kind === "user_input") {
    return waiting("Waiting for you");
  }
  if (work.wake.kind === "calendar_result") {
    return waiting("Waiting for Calendar");
  }
  const owner = ownerFromSessionWakeRef(work.wake.ref, ownerById);
  return waiting(
    owner ? `Waiting for ${owner.label}` : "Waiting for Session",
    owner?.sessionId,
  );
}

function projectReady(work: BrainActiveWork): Projection {
  if (!work.attention_pending) return problem("ready", "Next step unavailable");
  return {
    stage: "ready",
    tone: "attention",
    signalLabel:
      work.status === "needs_input" ? "Needs your input" : "Ready to continue",
    contradiction: false,
  };
}

function projectTerminal(work: BrainActiveWork): Projection {
  const stage = work.status === "done" ? "completed" : "cancelled";
  const finalizations = work.session_finalizations ?? [];
  const failed = finalizations.filter((item) => item.state === "failed").length;
  if (failed > 0) {
    return {
      stage,
      tone: "failed",
      signalLabel: "Couldn’t finish cleanly",
      detail: sessionCountLabel(failed, "needs review"),
      contradiction: false,
    };
  }
  const pending = finalizations.filter((item) => item.state === "pending").length;
  if (pending > 0) {
    return {
      stage,
      tone: "attention",
      signalLabel: "Wrapping up",
      detail: sessionCountLabel(pending, "closing"),
      contradiction: false,
    };
  }
  if (work.attention_pending) {
    return {
      stage,
      tone: "attention",
      signalLabel: "Result needs review",
      contradiction: false,
    };
  }
  return {
    stage,
    tone: work.status === "done" ? "complete" : "muted",
    signalLabel: work.status === "done" ? "Completed" : "Stopped",
    contradiction: false,
  };
}

function waiting(label: string, targetSessionId?: string): Projection {
  return {
    stage: "waiting",
    tone: "waiting",
    signalLabel: label,
    targetSessionId,
    contradiction: false,
  };
}

function problem(
  stage: WorkSignalStage,
  signalLabel: string,
  ownerSessionId?: string,
): Projection {
  return {
    stage,
    tone: "failed",
    signalLabel,
    detail: "Needs review",
    ownerSessionId,
    contradiction: true,
  };
}

function transitionKey(work: BrainActiveWork): string {
  return [
    work.work_id,
    work.revision,
    work.status,
    work.progress_mode ?? "terminal",
    work.owner_session_id ?? "",
    work.wake?.kind ?? "",
    work.attention_pending ? "ready" : "",
    (work.session_finalizations ?? []).map((item) => item.state).sort().join(","),
  ].join(":");
}

function ownerFromSessionWakeRef(
  ref: string,
  ownerById: ReadonlyMap<string, WorkSignalOwner>,
): WorkSignalOwner | undefined {
  let match: WorkSignalOwner | undefined;
  ownerById.forEach((owner) => {
    if (
      ref.startsWith(`session:${owner.sessionId}:turn:`) &&
      (!match || owner.sessionId.length > match.sessionId.length)
    ) {
      match = owner;
    }
  });
  return match;
}

function withAccessibility(
  item: Omit<WorkSignalItem, "accessibilityLabel">,
): WorkSignalItem {
  return {
    ...item,
    accessibilityLabel: [item.title, item.signalLabel, item.detail]
      .filter(Boolean)
      .join(", "),
  };
}

function summaryLabel(
  active: number,
  waitingCount: number,
  attentionCount: number,
  failureCount: number,
  outcomes: number,
): string {
  if (active === 0 && outcomes === 0) return "Work is clear";
  const activeLabel = countLabel(active, "active");
  if (failureCount > 0) {
    return `${activeLabel} · ${failureCount} ${failureCount === 1 ? "needs" : "need"} review`;
  }
  if (attentionCount > 0) return `${activeLabel} · ${countLabel(attentionCount, "ready")}`;
  if (waitingCount > 0) return `${activeLabel} · ${countLabel(waitingCount, "waiting")}`;
  if (active > 0 && outcomes > 0) return `${activeLabel} · ${countLabel(outcomes, "outcome")}`;
  return active > 0 ? activeLabel : countLabel(outcomes, "outcome");
}

function countLabel(count: number, noun: string): string {
  const plural = count !== 1 && noun !== "active" && noun !== "waiting" && noun !== "ready";
  return `${count} ${noun}${plural ? "s" : ""}`;
}

function sessionCountLabel(count: number, suffix: string): string {
  return `${count} Session${count === 1 ? "" : "s"} ${suffix}`;
}

function positiveNumber(value: number | undefined): number | undefined {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? value
    : undefined;
}
