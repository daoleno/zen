import type { CodexConversationEvent } from "../../services/codexConversation";
import type { BrainCurrentWork } from "../../store/brain";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import {
  brainWorkEventLifecycle,
  brainWorkEventSummary,
  brainWorkEventWorkTitle,
  brainWorkTitle,
  type BrainWorkLifecyclePresentation,
} from "./brainWorkEventPresentation";

export type BrainWorkResultGroup = {
  workId: string;
  events: BrainWorkResultEvent[];
  currentEvent: BrainWorkResultEvent;
  sourceCount?: number;
};

export type BrainWorkActivityRow = {
  id: string;
  title: string;
  summary?: string;
  updatedAt?: string;
  presentation: BrainWorkLifecyclePresentation;
  sourceCount?: number;
  event?: BrainWorkResultEvent;
  canOpenSession: boolean;
};

export type BrainWorkActivityModel = {
  active: BrainWorkActivityRow[];
  history: BrainWorkActivityRow[];
  activeCount: number;
  attentionCount: number;
  accessibilityLabel: string;
};

export function brainWorkEventFromConversationEvent(
  event: CodexConversationEvent,
): BrainWorkResultEvent | null {
  if (event.source !== "work_result" || event.kind !== "status") {
    return null;
  }
  const kind = normalizeResultKind(event.status);
  const reviewState = normalizeReviewState(event.work_review_state);
  const sessionState = normalizeSessionState(event.work_session_state);
  const workId = (event.work_id || "").trim();
  if (!kind || !reviewState || !sessionState || !workId) {
    return null;
  }
  return {
    event_id: event.id,
    kind,
    work_id: workId,
    work_title: (event.title || "").trim(),
    summary: (event.body || "").trim(),
    session_id: event.work_session_id?.trim() || undefined,
    session_name: event.session_name?.trim() || undefined,
    occurred_at: event.timestamp || new Date(0).toISOString(),
    unread: Boolean(event.unread),
    review_state: reviewState,
    session_state: sessionState,
    current_result: event.work_result_current === true,
  };
}

export function brainWorkEventsFromConversationEvents(
  events: CodexConversationEvent[],
): BrainWorkResultEvent[] {
  const workEvents: BrainWorkResultEvent[] = [];
  for (const event of events) {
    const projected = brainWorkEventFromConversationEvent(event);
    if (projected) {
      workEvents.push(projected);
    }
  }
  return workEvents;
}

export function groupBrainWorkEvents(
  events: BrainWorkResultEvent[],
): BrainWorkResultGroup[] {
  const groups: BrainWorkResultGroup[] = [];
  const byWorkId = new Map<string, number>();
  for (const event of events) {
    const index = byWorkId.get(event.work_id);
    if (index == null) {
      byWorkId.set(event.work_id, groups.length);
      groups.push({
        workId: event.work_id,
        events: [event],
        currentEvent: event,
        sourceCount: sourceCount([event]),
      });
      continue;
    }
    const group = groups[index];
    const nextEvents = group.events.concat(event);
    groups[index] = {
      ...group,
      events: nextEvents,
      currentEvent: preferCurrentEvent(group.currentEvent, event),
      sourceCount: sourceCount(nextEvents),
    };
  }
  return groups;
}

export function buildBrainWorkActivityModel({
  currentWork,
  resultEvents,
  openSessionIds,
  snapshotUpdatedAt,
}: {
  currentWork: BrainCurrentWork[];
  resultEvents: BrainWorkResultEvent[];
  openSessionIds: ReadonlySet<string>;
  snapshotUpdatedAt?: string;
}): BrainWorkActivityModel {
  const groups = groupBrainWorkEvents(resultEvents);
  const resultByWorkId = new Map(groups.map((group) => [group.workId, group]));
  const rows: BrainWorkActivityRow[] = [];
  const representedWorkIds = new Set<string>();

  for (const work of currentWork) {
    const group = resultByWorkId.get(work.work_id);
    representedWorkIds.add(work.work_id);
    rows.push(
      activityRowFromCurrentWork(
        work,
        group,
        openSessionIds,
        snapshotUpdatedAt,
      ),
    );
  }
  for (const group of groups) {
    if (!representedWorkIds.has(group.workId)) {
      rows.push(activityRowFromResult(group, openSessionIds));
    }
  }

  const active = rows.filter((row) => !row.presentation.terminal);
  const history = rows
    .filter((row) => row.presentation.terminal)
    .sort(compareRowsNewestFirst);
  const attentionCount = active.filter(
    (row) => row.presentation.lifecycle === "needs_you",
  ).length;
  const activeCount = active.length;
  return {
    active,
    history,
    activeCount,
    attentionCount,
    accessibilityLabel: workActivityAccessibilityLabel(
      activeCount,
      attentionCount,
      history.length,
    ),
  };
}

function activityRowFromCurrentWork(
  work: BrainCurrentWork,
  group: BrainWorkResultGroup | undefined,
  openSessionIds: ReadonlySet<string>,
  snapshotUpdatedAt: string | undefined,
): BrainWorkActivityRow {
  const event = group?.currentEvent;
  const presentation = currentWorkPresentation(work, event);
  const sessionId = event?.session_id || work.attempt_session_id;
  const sessionIds = new Set(
    [
      ...(group?.events.map((candidate) => candidate.session_id) ?? []),
      work.attempt_delegated ? work.attempt_session_id : undefined,
    ].filter((value): value is string => Boolean(value)),
  );
  return {
    id: work.work_id,
    title: event ? brainWorkEventWorkTitle(event) : brainWorkTitle(work.title),
    summary: event ? brainWorkEventSummary(event) || undefined : undefined,
    updatedAt: event?.occurred_at || snapshotUpdatedAt,
    presentation,
    sourceCount: sessionIds.size > 0 ? sessionIds.size : undefined,
    event,
    canOpenSession: Boolean(sessionId && openSessionIds.has(sessionId)),
  };
}

function activityRowFromResult(
  group: BrainWorkResultGroup,
  openSessionIds: ReadonlySet<string>,
): BrainWorkActivityRow {
  const event = group.currentEvent;
  return {
    id: group.workId,
    title: brainWorkEventWorkTitle(event),
    summary: brainWorkEventSummary(event) || undefined,
    updatedAt: event.occurred_at,
    presentation: brainWorkEventLifecycle(event),
    sourceCount: group.sourceCount,
    event,
    canOpenSession: Boolean(
      event.session_id && openSessionIds.has(event.session_id),
    ),
  };
}

function currentWorkPresentation(
  work: BrainCurrentWork,
  event: BrainWorkResultEvent | undefined,
): BrainWorkLifecyclePresentation {
  const eventPresentation = event ? brainWorkEventLifecycle(event) : undefined;
  if (
    eventPresentation?.lifecycle === "failed" ||
    eventPresentation?.lifecycle === "needs_you"
  ) {
    return eventPresentation;
  }
  if (work.status === "needs_input") {
    return presentation("needs_you");
  }
  if (work.status === "cancelled") {
    return presentation("failed");
  }
  if (work.status === "done") {
    return presentation("done");
  }
  if (work.attention_state === "reviewing") {
    return presentation("reviewing");
  }
  if (
    work.attention_state === "queued" ||
    work.attention_state === "reserved" ||
    work.progress_mode === "ready"
  ) {
    return presentation("ready");
  }
  return eventPresentation ?? presentation("working");
}

function presentation(
  lifecycle: BrainWorkLifecyclePresentation["lifecycle"],
): BrainWorkLifecyclePresentation {
  switch (lifecycle) {
    case "needs_you":
      return {
        lifecycle,
        label: "Needs you",
        icon: "help-circle-outline",
        tone: "attention",
        terminal: false,
      };
    case "failed":
      return {
        lifecycle,
        label: "Failed",
        icon: "alert-circle-outline",
        tone: "danger",
        terminal: true,
      };
    case "done":
      return {
        lifecycle,
        label: "Done",
        icon: "checkmark-circle-outline",
        tone: "neutral",
        terminal: true,
      };
    case "reviewing":
      return {
        lifecycle,
        label: "Reviewing",
        icon: "eye-outline",
        tone: "accent",
        terminal: false,
      };
    case "ready":
      return {
        lifecycle,
        label: "Ready",
        icon: "checkmark-circle-outline",
        tone: "accent",
        terminal: false,
      };
    case "working":
      return {
        lifecycle,
        label: "Working",
        icon: "ellipsis-horizontal-circle-outline",
        tone: "neutral",
        terminal: false,
      };
  }
}

function preferCurrentEvent(
  current: BrainWorkResultEvent,
  candidate: BrainWorkResultEvent,
): BrainWorkResultEvent {
  if (candidate.current_result !== current.current_result) {
    return candidate.current_result ? candidate : current;
  }
  return candidate;
}

function sourceCount(events: BrainWorkResultEvent[]): number | undefined {
  const ids = new Set<string>();
  for (const event of events) {
    if (event.session_id) {
      ids.add(event.session_id);
    }
  }
  return ids.size > 0 ? ids.size : undefined;
}

function compareRowsNewestFirst(
  left: BrainWorkActivityRow,
  right: BrainWorkActivityRow,
) {
  return (right.updatedAt || "").localeCompare(left.updatedAt || "");
}

function workActivityAccessibilityLabel(
  activeCount: number,
  attentionCount: number,
  historyCount: number,
) {
  const parts = ["Work activity"];
  if (activeCount > 0) {
    parts.push(`${activeCount} active`);
  }
  if (attentionCount > 0) {
    parts.push(`${attentionCount} need you`);
  }
  if (historyCount > 0) {
    parts.push(`${historyCount} in history`);
  }
  return parts.join(". ");
}

function normalizeReviewState(value: string | undefined) {
  return value === "queued" ||
    value === "reserved" ||
    value === "reviewing" ||
    value === "resolved"
    ? value
    : null;
}

function normalizeSessionState(value: string | undefined) {
  return value === "open" ||
    value === "closing" ||
    value === "finalized" ||
    value === "close_failed" ||
    value === "not_required"
    ? value
    : null;
}

function normalizeResultKind(value: string | undefined) {
  return value === "session.done" ||
    value === "session.failed" ||
    value === "session.needs_input" ||
    value === "session.stale" ||
    value === "session.uncertain" ||
    value === "session.ownership_lost"
    ? value
    : null;
}
