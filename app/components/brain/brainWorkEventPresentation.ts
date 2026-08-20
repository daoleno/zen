import type { BrainWorkResultEvent } from "./brainWorkEvent";

const CANONICAL_SESSION_SUFFIX =
  /\s*\(brain-agent-[^()\s]+:@\d+\)\s*$/i;
const CANONICAL_SESSION_ID = /brain-agent-[^()\s]+:@\d+/i;
const CANONICAL_SESSION_ID_GLOBAL = /brain-agent-[^()\s]+:@\d+/gi;

export function brainWorkEventWorkTitle(event: BrainWorkResultEvent): string {
  const normalized = event.work_title
    .replace(CANONICAL_SESSION_SUFFIX, "")
    .trim();
  return normalized || "Delegated Work";
}

export function brainWorkEventSummary(event: BrainWorkResultEvent): string {
  return event.summary
    .replace(CANONICAL_SESSION_ID_GLOBAL, "the session")
    .replace(/\bdelegated session\b/gi, "the session")
    .replace(/\s+/g, " ")
    .trim();
}

export function brainWorkEventReviewLabel(
  event: BrainWorkResultEvent,
): string {
  switch (event.review_state) {
    case "queued":
      return "Needs review";
    case "reserved":
      return "Queued";
    case "reviewing":
      return "Reviewing";
    case "resolved":
      return "Reviewed";
  }
}

export function brainWorkEventSessionLabel(
  event: BrainWorkResultEvent,
): string | undefined {
  switch (event.session_state) {
    case "open":
    case "closing":
    case "finalized":
    case "close_failed":
      return undefined;
    case "not_required":
      return undefined;
  }
}

export function brainWorkEventSourceLabel(
  event: BrainWorkResultEvent,
): string | undefined {
  const rawName = event.session_name?.trim() || "";
  if (!rawName) {
    return undefined;
  }
  const normalized = rawName.replace(CANONICAL_SESSION_SUFFIX, "").trim();
  if (!normalized || CANONICAL_SESSION_ID.test(normalized)) {
    return undefined;
  }
  if (
    normalized.toLowerCase() ===
    brainWorkEventWorkTitle(event).toLowerCase()
  ) {
    return undefined;
  }
  return normalized;
}

export function brainWorkEventAccessibilityLabel({
  event,
  statusLabel,
  occurredAtLabel,
}: {
  event: BrainWorkResultEvent;
  statusLabel: string;
  occurredAtLabel: string;
}) {
  const source = brainWorkEventSourceLabel(event);
  const workTitle = brainWorkEventWorkTitle(event);
  const summary = brainWorkEventSummary(event);
  const review = brainWorkEventReviewLabel(event);
  const sessionState = brainWorkEventSessionLabel(event);
  return [
    statusLabel,
    `Work ${workTitle}`,
    summary,
    review,
    sessionState ?? "",
    event.current_result ? "Current result" : "Superseded result",
    source ? `Source: ${source}` : "",
    occurredAtLabel,
    event.unread ? "Unread result" : "Read result",
  ]
    .filter(Boolean)
    .join(". ")
    .replace(CANONICAL_SESSION_ID_GLOBAL, "the session")
    .replace(/\bdelegated session\b/gi, "the session");
}
