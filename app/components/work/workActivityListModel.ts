import type { AgentStatus } from "../../constants/tokens";
import type { BrainCurrentWork } from "../../store/brain";
import { brainWorkTitle } from "../brain/brainWorkEventPresentation";

export type WorkActivityLifecycle =
  | "needs_you"
  | "reviewing"
  | "ready"
  | "working"
  | "waiting"
  | "done"
  | "cancelled";

export type WorkActivityTone =
  | "attention"
  | "accent"
  | "neutral"
  | "danger";

export type WorkActivityOwner = {
  sessionId: string;
  title: string;
  status: AgentStatus;
  delegated: boolean;
};

export type WorkActivityRow = {
  id: string;
  title: string;
  lifecycle: WorkActivityLifecycle;
  statusLabel: string;
  tone: WorkActivityTone;
  terminal: boolean;
  unread: boolean;
  owner?: WorkActivityOwner;
  action: "open_session" | "open_brain" | "none";
};

export type WorkActivityListModel = {
  attention: WorkActivityRow[];
  active: WorkActivityRow[];
  recent: WorkActivityRow[];
  historicalResultCount: number;
  totalVisible: number;
  accessibilityLabel: string;
};

export function buildWorkActivityListModel({
  work,
  owners,
  historicalResultCount,
}: {
  work: readonly BrainCurrentWork[];
  owners: readonly WorkActivityOwner[];
  historicalResultCount: number;
}): WorkActivityListModel {
  const ownerById = new Map(
    owners
      .filter((owner) => owner.delegated)
      .map((owner) => [owner.sessionId, owner] as const),
  );
  const rows = work.map((item) => workActivityRow(item, ownerById));
  const attention = rows
    .filter((row) => row.lifecycle === "needs_you")
    .sort(compareRows);
  const active = rows
    .filter((row) => !row.terminal && row.lifecycle !== "needs_you")
    .sort(compareRows);
  const recent = rows.filter((row) => row.terminal).sort(compareRows);
  const normalizedHistoryCount = Math.max(0, Math.floor(historicalResultCount));
  const parts = ["Work activity"];
  if (attention.length > 0) parts.push(`${attention.length} need you`);
  if (active.length > 0) parts.push(`${active.length} active`);
  if (recent.length + normalizedHistoryCount > 0) {
    parts.push(`${recent.length + normalizedHistoryCount} in history`);
  }
  return {
    attention,
    active,
    recent,
    historicalResultCount: normalizedHistoryCount,
    totalVisible: rows.length,
    accessibilityLabel: parts.join(". "),
  };
}

function workActivityRow(
  work: BrainCurrentWork,
  ownerById: ReadonlyMap<string, WorkActivityOwner>,
): WorkActivityRow {
  const owner = resolveOwner(work, ownerById);
  const presentation = workPresentation(work);
  return {
    id: work.work_id,
    title: brainWorkTitle(work.title),
    ...presentation,
    unread: work.unread_result,
    owner,
    action: owner
      ? "open_session"
      : presentation.lifecycle === "needs_you"
        ? "open_brain"
        : "none",
  };
}

function workPresentation(work: BrainCurrentWork): Pick<
  WorkActivityRow,
  "lifecycle" | "statusLabel" | "tone" | "terminal"
> {
  if (work.status === "done") {
    return {
      lifecycle: "done",
      statusLabel: "Done",
      tone: "neutral",
      terminal: true,
    };
  }
  if (work.status === "cancelled") {
    return {
      lifecycle: "cancelled",
      statusLabel: "Cancelled",
      tone: "danger",
      terminal: true,
    };
  }
  if (
    work.status === "needs_input" ||
    work.wake?.kind === "user_input"
  ) {
    return {
      lifecycle: "needs_you",
      statusLabel: "Needs you",
      tone: "attention",
      terminal: false,
    };
  }
  if (work.attention_state === "reviewing") {
    return {
      lifecycle: "reviewing",
      statusLabel: "Reviewing",
      tone: "accent",
      terminal: false,
    };
  }
  if (
    work.attention_state === "queued" ||
    work.attention_state === "reserved" ||
    work.progress_mode === "ready"
  ) {
    return {
      lifecycle: "ready",
      statusLabel: "Ready",
      tone: "accent",
      terminal: false,
    };
  }
  if (work.status === "waiting" || work.progress_mode === "waiting") {
    return {
      lifecycle: "waiting",
      statusLabel: "Waiting",
      tone: "neutral",
      terminal: false,
    };
  }
  return {
    lifecycle: "working",
    statusLabel: "Working",
    tone: "neutral",
    terminal: false,
  };
}

function resolveOwner(
  work: BrainCurrentWork,
  ownerById: ReadonlyMap<string, WorkActivityOwner>,
): WorkActivityOwner | undefined {
  const attemptSessionId = work.attempt_session_id?.trim();
  if (work.attempt_delegated && attemptSessionId) {
    const owner = ownerById.get(attemptSessionId);
    if (owner) return owner;
  }
  const wakeRef = work.wake?.kind === "session_terminal" ? work.wake.ref : "";
  if (!wakeRef) return undefined;
  let match: WorkActivityOwner | undefined;
  ownerById.forEach((owner) => {
    if (
      wakeRef.startsWith(`session:${owner.sessionId}:turn:`) &&
      (!match || owner.sessionId.length > match.sessionId.length)
    ) {
      match = owner;
    }
  });
  return match;
}

function compareRows(left: WorkActivityRow, right: WorkActivityRow): number {
  return (
    Number(right.unread) - Number(left.unread) ||
    left.title.localeCompare(right.title) ||
    left.id.localeCompare(right.id)
  );
}
