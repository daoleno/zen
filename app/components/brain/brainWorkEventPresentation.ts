import type { BrainWorkResultEvent } from "./brainWorkEvent";
import type { BrainCurrentWork } from "../../store/brain";

export type BrainWorkLifecycle =
  | "working"
  | "ready"
  | "reviewing"
  | "waiting"
  | "done"
  | "cancelled"
  | "needs_you"
  | "failed";

export type BrainWorkLifecyclePresentation = {
  lifecycle: BrainWorkLifecycle;
  label:
    | "Working"
    | "Ready"
    | "Reviewing"
    | "Waiting"
    | "Done"
    | "Cancelled"
    | "Needs you"
    | "Failed";
  icon:
    | "ellipsis-horizontal-circle-outline"
    | "checkmark-circle-outline"
    | "eye-outline"
    | "time-outline"
    | "close-circle-outline"
    | "help-circle-outline"
    | "alert-circle-outline";
  tone: "neutral" | "accent" | "attention" | "danger";
  terminal: boolean;
};

const CANONICAL_SESSION_SUFFIX =
  /\s*\(brain-agent-[^()\s]+:@\d+\)\s*$/i;
const CANONICAL_SESSION_ID = /brain-agent-[^()\s]+:@\d+/i;
const CANONICAL_SESSION_ID_GLOBAL = /brain-agent-[^()\s]+:@\d+/gi;
const PROVIDER_TURN_ID_GLOBAL = /\bturn:[a-z0-9-]+\b/gi;

export function brainWorkEventWorkTitle(event: BrainWorkResultEvent): string {
  return brainWorkTitle(event.work_title);
}

export function brainWorkTitle(value: string): string {
  const normalized = value
    .replace(CANONICAL_SESSION_SUFFIX, "")
    .replace(PROVIDER_TURN_ID_GLOBAL, "")
    .replace(/\s+/g, " ")
    .trim();
  return normalized || "Delegated Work";
}

export function brainWorkEventSummary(event: BrainWorkResultEvent): string {
  return event.summary
    .replace(CANONICAL_SESSION_ID_GLOBAL, "the session")
    .replace(PROVIDER_TURN_ID_GLOBAL, "the provider turn")
    .replace(/\bdelegated session\b/gi, "the session")
    .replace(/\s+/g, " ")
    .trim();
}

export function brainWorkEventLifecycle(
  event: BrainWorkResultEvent,
): BrainWorkLifecyclePresentation {
  if (
    event.kind === "session.failed" ||
    event.kind === "session.ownership_lost"
  ) {
    return {
      lifecycle: "failed",
      label: "Failed",
      icon: "alert-circle-outline",
      tone: "danger",
      terminal: true,
    };
  }
  if (event.review_state === "reviewing") {
    return {
      lifecycle: "reviewing",
      label: "Reviewing",
      icon: "eye-outline",
      tone: "accent",
      terminal: false,
    };
  }
  if (event.review_state === "resolved") {
    return {
      lifecycle: "done",
      label: "Done",
      icon: "checkmark-circle-outline",
      tone: "neutral",
      terminal: true,
    };
  }
  if (event.kind === "session.needs_input") {
    return {
      lifecycle: "needs_you",
      label: "Needs you",
      icon: "help-circle-outline",
      tone: "attention",
      terminal: false,
    };
  }
  if (event.kind === "session.done") {
    return {
      lifecycle: "ready",
      label: "Ready",
      icon: "checkmark-circle-outline",
      tone: "accent",
      terminal: false,
    };
  }
  return {
    lifecycle: "working",
    label: "Working",
    icon: "ellipsis-horizontal-circle-outline",
    tone: "neutral",
    terminal: false,
  };
}

export function brainCurrentWorkLifecycle(
  work: BrainCurrentWork,
  event?: BrainWorkResultEvent,
): BrainWorkLifecyclePresentation {
  if (work.status === "needs_input") {
    return lifecyclePresentation("needs_you");
  }
  if (work.status === "cancelled") {
    return lifecyclePresentation("cancelled");
  }
  if (work.status === "done") {
    return lifecyclePresentation("done");
  }
  if (work.attention_state === "reviewing") {
    return lifecyclePresentation("reviewing");
  }
  if (
    work.attention_state === "queued" ||
    work.attention_state === "reserved" ||
    work.progress_mode === "ready"
  ) {
    return lifecyclePresentation("ready");
  }
  if (work.status === "waiting" || work.progress_mode === "waiting") {
    return lifecyclePresentation("waiting");
  }
  const eventPresentation = event ? brainWorkEventLifecycle(event) : undefined;
  if (eventPresentation?.lifecycle === "needs_you") {
    return eventPresentation;
  }
  if (
    eventPresentation?.lifecycle === "ready" ||
    eventPresentation?.lifecycle === "reviewing"
  ) {
    return eventPresentation;
  }
  return lifecyclePresentation("working");
}

export function brainWorkAgentCountLabel(count: number): string {
  return `${count} ${count === 1 ? "agent" : "agents"}`;
}

function lifecyclePresentation(
  lifecycle: BrainWorkLifecycle,
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
    case "cancelled":
      return {
        lifecycle,
        label: "Cancelled",
        icon: "close-circle-outline",
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
    case "waiting":
      return {
        lifecycle,
        label: "Waiting",
        icon: "time-outline",
        tone: "neutral",
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
    .replace(CANONICAL_SESSION_ID_GLOBAL, "the session")
    .replace(PROVIDER_TURN_ID_GLOBAL, "the provider turn")
    .replace(/\bdelegated session\b/gi, "the session");
}
