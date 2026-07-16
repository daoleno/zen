const ATTACHMENT_TAG_RE = /<zen_attachments>\s*([\s\S]*?)\s*<\/zen_attachments>/i;

export const PENDING_USER_MESSAGE_SENDING_MAX_AGE_MS = 45_000;
export const PENDING_USER_MESSAGE_QUEUED_MAX_AGE_MS = 30 * 60_000;
export const PENDING_MESSAGE_RETRY_ACCESSIBILITY_LABEL =
  "Retry sending message";

export type PendingUserMessageLifecycle =
  | "unconfirmed"
  | "sending"
  | "queued"
  | "failed"
  | "settled";

export type PendingUserMessageLifecycleFields = {
  id: string;
  turnId: string;
  turnStartedAt: string;
  body: string;
  sentText: string;
  createdAt: string;
  lifecycle: PendingUserMessageLifecycle;
  queuedHint?: boolean;
  acceptedAt?: string;
  dispatchRequestId?: string;
  lastAttemptAt?: string;
  failureCode?: string;
  failureMessage?: string;
  failedAt?: string;
  attachments?: Array<{ path?: string }>;
  confirmedEventId?: string;
  createdAfterMaxSeq?: number;
  createdAfterEventIds?: string[];
};

export function markPendingUserMessageDispatched<
  T extends PendingUserMessageLifecycleFields,
>(
  message: T,
  attempt: {
    requestId: string;
    attemptedAt: string;
    queuedHint?: boolean;
    createdAfterMaxSeq?: number;
    createdAfterEventIds?: string[];
  },
): T {
  return {
    ...message,
    lifecycle: "unconfirmed",
    dispatchRequestId: attempt.requestId,
    lastAttemptAt: attempt.attemptedAt,
    queuedHint: attempt.queuedHint,
    createdAt: attempt.attemptedAt,
    acceptedAt: undefined,
    failureCode: undefined,
    failureMessage: undefined,
    failedAt: undefined,
    createdAfterMaxSeq: attempt.createdAfterMaxSeq,
    createdAfterEventIds: attempt.createdAfterEventIds,
  };
}

export function redispatchPendingUserMessageInSubmissionOrder<
  T extends PendingUserMessageLifecycleFields,
>(
  messages: T[],
  id: string,
  attempt: Parameters<typeof markPendingUserMessageDispatched<T>>[1],
): T[] {
  const message = messages.find((candidate) => candidate.id === id);
  if (!message) {
    return messages;
  }
  return messages.map((candidate) =>
    candidate.id === id
      ? markPendingUserMessageDispatched(candidate, attempt)
      : candidate
  );
}

export function rejectPendingUserMessage<
  T extends PendingUserMessageLifecycleFields,
>(
  message: T,
  rejection: {
    requestId: string;
    code: string;
    message: string;
    failedAt: string;
  },
): T {
  if (
    message.dispatchRequestId !== rejection.requestId ||
    message.acceptedAt
  ) {
    return message;
  }
  return {
    ...message,
    lifecycle: "failed",
    failureCode: rejection.code,
    failureMessage: rejection.message,
    failedAt: rejection.failedAt,
  };
}

export type PendingLifecyclePresentation = {
  lifecycle: PendingUserMessageLifecycle;
  label: string;
  accessibilityLabel: string;
};

export function pendingUserMessageLifecycleLabel(
  lifecycle: PendingUserMessageLifecycle,
  queuedOrdinal: number = 0,
  queuedCount: number = 0,
): string {
  if (lifecycle === "sending") {
    return "Sending";
  }
  if (lifecycle === "unconfirmed") {
    return "Sent · unconfirmed";
  }
  if (lifecycle === "failed") {
    return "Not accepted";
  }
  if (lifecycle === "settled") {
    return "";
  }
  if (queuedCount > 1) {
    return `Queued ${queuedOrdinal + 1}/${queuedCount}`;
  }
  return queuedOrdinal <= 0 ? "Queued next" : "Queued";
}

export function pendingUserMessageLifecycleAccessibilityLabel(
  lifecycle: PendingUserMessageLifecycle,
  queuedOrdinal: number = 0,
  queuedCount: number = 0,
): string {
  if (lifecycle === "queued" && queuedCount > 1) {
    return `Queued, ${queuedOrdinal + 1} of ${queuedCount}`;
  }
  if (lifecycle === "unconfirmed") {
    return "Sent, confirmation pending";
  }
  if (lifecycle === "failed") {
    return "Message not accepted";
  }
  return pendingUserMessageLifecycleLabel(
    lifecycle,
    queuedOrdinal,
    queuedCount,
  );
}

export function pendingUserMessageMaxAgeMs(
  lifecycle: PendingUserMessageLifecycle = "sending",
): number {
  if (
    lifecycle === "settled" ||
    lifecycle === "unconfirmed" ||
    lifecycle === "failed"
  ) {
    return Number.POSITIVE_INFINITY;
  }
  return lifecycle === "queued"
    ? PENDING_USER_MESSAGE_QUEUED_MAX_AGE_MS
    : PENDING_USER_MESSAGE_SENDING_MAX_AGE_MS;
}

export function shouldPrunePendingUserMessageByLifecycle(
  message: Pick<PendingUserMessageLifecycleFields, "createdAt" | "lifecycle">,
  now: number,
): boolean {
  const createdAt = new Date(message.createdAt).getTime();
  if (!Number.isFinite(createdAt)) {
    return false;
  }
  return now - createdAt > pendingUserMessageMaxAgeMs(message.lifecycle);
}

export function nextPendingUserMessagePruneAt(
  messages: Array<
    Pick<PendingUserMessageLifecycleFields, "createdAt" | "lifecycle">
  >,
  now: number,
): number | undefined {
  const nextPruneAt = messages.reduce((soonest, message) => {
    const createdAt = new Date(message.createdAt).getTime();
    const maxAgeMs = pendingUserMessageMaxAgeMs(message.lifecycle);
    const maxAgeAt = Number.isFinite(createdAt)
      ? createdAt + maxAgeMs
      : now + maxAgeMs;
    return Math.min(soonest, maxAgeAt);
  }, Number.POSITIVE_INFINITY);
  return Number.isFinite(nextPruneAt) ? nextPruneAt : undefined;
}

export function retainPendingUserMessages<
  T extends PendingUserMessageLifecycleFields,
>(
  messages: T[],
  now: number,
  orphanLimit: number = 12,
): T[] {
  const retained = messages.filter(
    (message) =>
      Boolean(message.acceptedAt) ||
      !shouldPrunePendingUserMessageByLifecycle(message, now),
  );
  let orphanBudget = Math.max(0, orphanLimit);
  const keep = new Set<string>();
  for (let index = retained.length - 1; index >= 0; index -= 1) {
    const message = retained[index];
    const durableUnknownDisposition =
      message.lifecycle === "unconfirmed" || message.lifecycle === "failed";
    if (
      message.acceptedAt ||
      durableUnknownDisposition ||
      orphanBudget > 0
    ) {
      keep.add(message.id);
      if (!message.acceptedAt && !durableUnknownDisposition) {
        orphanBudget -= 1;
      }
    }
  }
  return retained.filter((message) => keep.has(message.id));
}

export function queuedOrdinalByPendingId(
  pendingUserMessages: Array<
    Pick<PendingUserMessageLifecycleFields, "id" | "lifecycle" | "confirmedEventId">
  >,
): Map<string, number> {
  const ordinals = new Map<string, number>();
  let nextOrdinal = 0;
  for (const message of pendingUserMessages) {
    if (message.lifecycle !== "queued") {
      continue;
    }
    ordinals.set(message.id, nextOrdinal);
    nextOrdinal += 1;
  }
  return ordinals;
}

export function presentPendingUserMessageLifecycle(
  message: Pick<
    PendingUserMessageLifecycleFields,
    "id" | "lifecycle" | "confirmedEventId"
  >,
  queuedOrdinals: Map<string, number>,
): PendingLifecyclePresentation {
  const queuedOrdinal = queuedOrdinals.get(message.id) ?? 0;
  const label = pendingUserMessageLifecycleLabel(
    message.lifecycle,
    queuedOrdinal,
    queuedOrdinals.size,
  );
  return {
    lifecycle: message.lifecycle,
    label,
    accessibilityLabel: pendingUserMessageLifecycleAccessibilityLabel(
      message.lifecycle,
      queuedOrdinal,
      queuedOrdinals.size,
    ),
  };
}

export type ReconcileUserEvent = {
  id: string;
  seq?: number;
  position?: number;
  submission_id?: string;
  submission_state?: string;
  kind: string;
  body?: string;
  files?: string[];
};

export function reconcilePendingUserMessagesAgainstEvents<
  T extends PendingUserMessageLifecycleFields,
>(pendingUserMessages: T[], events: ReconcileUserEvent[]): T[] {
  if (pendingUserMessages.length === 0 || events.length === 0) {
    return pendingUserMessages;
  }
  const userEvents = events.filter((event) => event.kind === "user_message");
  if (userEvents.length === 0) {
    return pendingUserMessages;
  }
  const usedEventIds = new Set(
    pendingUserMessages
      .map((message) => message.confirmedEventId)
      .filter((id): id is string => Boolean(id)),
  );
  const reconciled: T[] = [];
  for (const message of pendingUserMessages) {
    if (message.confirmedEventId) {
      reconciled.push(message);
      continue;
    }
    const previousEventIds = new Set(message.createdAfterEventIds ?? []);
    const isAvailable = (event: ReconcileUserEvent) => {
      if (!event.id || usedEventIds.has(event.id) || previousEventIds.has(event.id)) {
        return false;
      }

      if (
        event.id === message.id ||
        event.id === message.turnId ||
        event.submission_id === message.id ||
        event.submission_id === message.turnId
      ) {
        return true;
      }
      if (
        typeof message.createdAfterMaxSeq === "number" &&
        Number.isFinite(message.createdAfterMaxSeq) &&
        typeof event.seq === "number" &&
        event.seq <= message.createdAfterMaxSeq
      ) {
        return false;
      }
      return true;
    };
    // Canonical identity wins. The provider-safe fallback is causal FIFO: a
    // body or attachment signature is never an identity key.
    const confirmedEvent = userEvents.find((event) =>
      isAvailable(event) &&
      (event.id === message.id ||
        event.id === message.turnId ||
        event.submission_id === message.id ||
        event.submission_id === message.turnId)
    ) ?? userEvents.find(isAvailable);
    if (!confirmedEvent) {
      reconciled.push(message);
      continue;
    }
    usedEventIds.add(confirmedEvent.id);
    reconciled.push({
      ...message,
      confirmedEventId: confirmedEvent.id,
    });
  }
  return reconciled;
}

export function comparableUserMessageText(value: string) {
  return value
    .replace(ATTACHMENT_TAG_RE, "")
    .replace(/\s+/g, " ")
    .trim();
}
