import type { BrainWorkResultEvent } from "../../store/brain";

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
  return event.summary.replace(
    CANONICAL_SESSION_ID_GLOBAL,
    "Delegated Session",
  );
}

export function brainWorkEventSourceLabel(
  event: BrainWorkResultEvent,
): string | undefined {
  const rawName = event.session_name?.trim() || "";
  if (!rawName) {
    return event.session_id ? "Delegated Session" : undefined;
  }
  const normalized = rawName.replace(CANONICAL_SESSION_SUFFIX, "").trim();
  if (!normalized || CANONICAL_SESSION_ID.test(normalized)) {
    return event.session_id ? "Delegated Session" : undefined;
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
  return [
    statusLabel,
    `Work ${workTitle}`,
    summary,
    source ? `Source: ${source}` : "",
    occurredAtLabel,
    event.unread ? "Unread result" : "Read result",
  ]
    .filter(Boolean)
    .join(". ")
    .replace(CANONICAL_SESSION_ID_GLOBAL, "Delegated Session");
}
