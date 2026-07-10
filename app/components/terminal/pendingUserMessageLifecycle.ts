const ATTACHMENT_TAG_RE = /<zen_attachments>\s*([\s\S]*?)\s*<\/zen_attachments>/i;

export const PENDING_USER_MESSAGE_SENDING_MAX_AGE_MS = 45_000;
export const PENDING_USER_MESSAGE_QUEUED_MAX_AGE_MS = 30 * 60_000;

export type PendingUserMessageLifecycle = "sending" | "queued";

export type PendingUserMessageLifecycleFields = {
  id: string;
  body: string;
  sentText: string;
  createdAt: string;
  lifecycle: PendingUserMessageLifecycle;
  confirmedEventId?: string;
  createdAfterMaxSeq?: number;
  createdAfterEventIds?: string[];
};

export type PendingLifecyclePresentation = {
  lifecycle: PendingUserMessageLifecycle;
  label: string;
  accessibilityLabel: string;
};

export function classifyPendingUserMessageLifecycle(
  turnBusy: boolean,
): PendingUserMessageLifecycle {
  return turnBusy ? "queued" : "sending";
}

export function pendingUserMessageLifecycleLabel(
  lifecycle: PendingUserMessageLifecycle,
  queuedOrdinal: number = 0,
): string {
  if (lifecycle === "sending") {
    return "Sending";
  }
  return queuedOrdinal <= 0 ? "Queued next" : "Queued";
}

export function pendingUserMessageLifecycleAccessibilityLabel(
  lifecycle: PendingUserMessageLifecycle,
  queuedOrdinal: number = 0,
): string {
  return pendingUserMessageLifecycleLabel(lifecycle, queuedOrdinal);
}

export function pendingUserMessageMaxAgeMs(
  lifecycle: PendingUserMessageLifecycle = "sending",
): number {
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

export function queuedOrdinalByPendingId(
  pendingUserMessages: Array<
    Pick<PendingUserMessageLifecycleFields, "id" | "lifecycle" | "confirmedEventId">
  >,
): Map<string, number> {
  const ordinals = new Map<string, number>();
  let nextOrdinal = 0;
  for (const message of pendingUserMessages) {
    if (message.confirmedEventId || message.lifecycle !== "queued") {
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
  );
  return {
    lifecycle: message.lifecycle,
    label,
    accessibilityLabel: pendingUserMessageLifecycleAccessibilityLabel(
      message.lifecycle,
      queuedOrdinal,
    ),
  };
}

export type ReconcileUserEvent = {
  id: string;
  seq?: number;
  kind: string;
  body?: string;
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
      continue;
    }
    const previousEventIds = new Set(message.createdAfterEventIds ?? []);
    const sentText = comparableUserMessageText(message.sentText);
    const body = comparableUserMessageText(message.body);
    const confirmedEvent = userEvents.find((event) => {
      if (!event.id || usedEventIds.has(event.id) || previousEventIds.has(event.id)) {
        return false;
      }
      if (
        typeof message.createdAfterMaxSeq === "number" &&
        Number.isFinite(message.createdAfterMaxSeq) &&
        typeof event.seq === "number" &&
        event.seq <= message.createdAfterMaxSeq
      ) {
        return false;
      }
      const eventText = comparableUserMessageText(event.body || "");
      return Boolean(
        eventText &&
          ((sentText && eventText === sentText) ||
            (body && eventText === body)),
      );
    });
    if (!confirmedEvent) {
      reconciled.push(message);
      continue;
    }
    usedEventIds.add(confirmedEvent.id);
  }
  return reconciled;
}

export function comparableUserMessageText(value: string) {
  return value
    .replace(ATTACHMENT_TAG_RE, "")
    .replace(/\s+/g, " ")
    .trim();
}
