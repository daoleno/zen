import type { CodexConversationEvent } from "../../services/codexConversation";
import type { BrainWorkResultEvent } from "./brainWorkEvent";

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
