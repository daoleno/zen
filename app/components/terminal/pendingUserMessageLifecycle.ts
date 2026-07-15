import {
  isStructuredTurnRunning,
  type StructuredTurn,
} from "../../services/codexConversation";

const ATTACHMENT_TAG_RE = /<zen_attachments>\s*([\s\S]*?)\s*<\/zen_attachments>/i;

export const PENDING_USER_MESSAGE_SENDING_MAX_AGE_MS = 45_000;
export const PENDING_USER_MESSAGE_QUEUED_MAX_AGE_MS = 30 * 60_000;

export type PendingUserMessageLifecycle = "sending" | "queued" | "settled";

export type PendingUserMessageLifecycleFields = {
  id: string;
  turnId: string;
  turnStartedAt: string;
  body: string;
  sentText: string;
  createdAt: string;
  lifecycle: PendingUserMessageLifecycle;
  acceptedAt?: string;
  attachments?: Array<{ path?: string }>;
  confirmedEventId?: string;
  authoritativeQueueObserved?: boolean;
  authoritativeActiveObserved?: boolean;
  authoritativeLifecycleEpoch?: string;
  authoritativeLifecycleRevision?: number;
  createdAfterMaxSeq?: number;
  createdAfterEventIds?: string[];
};

export function reconcilePendingUserMessagesWithStructuredTurns<
  T extends PendingUserMessageLifecycleFields,
>(
  pendingUserMessages: T[],
  turn?: StructuredTurn,
  queuedTurns?: StructuredTurn[],
  lifecycleEpoch?: string,
  lifecycleRevision?: number,
): T[] {
  if (pendingUserMessages.length === 0) {
    return pendingUserMessages;
  }
  const activeTurnId = isStructuredTurnRunning(turn) ? turn.id : undefined;
  const terminalTurnId = turn && !isStructuredTurnRunning(turn)
    ? turn.id
    : undefined;
  const queuedOrder = new Map(
    (queuedTurns ?? []).map((queuedTurn, index) => [queuedTurn.id, index]),
  );
  let changed = false;
  const retiredConfirmedEventIds: string[] = [];
  const originalOrder = new Map(
    pendingUserMessages.map((message, index) => [message.id, index]),
  );
  const reconciled = pendingUserMessages.flatMap((message) => {
    const active = activeTurnId === message.turnId;
    const queued = queuedOrder.has(message.turnId);
    const terminal = terminalTurnId === message.turnId;
    const wasAuthoritativelyObserved = Boolean(
      message.authoritativeQueueObserved ||
        message.authoritativeActiveObserved,
    );

    // A confirmed echo owns durable rendering as soon as this turn is
    // promoted. While it remains queued, however, the accepted queue record
    // must continue to overlay the provider echo with Queued metadata.
    const projectionAdvancedPastMessage = lifecycleProjectionIsNewer(
      message.authoritativeLifecycleEpoch,
      message.authoritativeLifecycleRevision,
      lifecycleEpoch,
      lifecycleRevision,
    );
    const advancedBeyondMessage = !active && !queued &&
      wasAuthoritativelyObserved && projectionAdvancedPastMessage;
    const shouldRetire = Boolean(message.confirmedEventId) &&
      (terminal || active || advancedBeyondMessage);
    if (shouldRetire) {
      changed = true;
      if (message.confirmedEventId) {
        retiredConfirmedEventIds.push(message.confirmedEventId);
      }
      return [];
    }

    const lifecycle = active
      ? "sending"
      : queued
        ? "queued"
        : terminal || advancedBeyondMessage
          ? "settled"
          : message.lifecycle;
    const authoritativeQueueObserved = queued ||
      message.authoritativeQueueObserved;
    const authoritativeActiveObserved = active ||
      message.authoritativeActiveObserved;
    const authoritativeLifecycleEpoch = active || queued || terminal || advancedBeyondMessage
      ? lifecycleEpoch ?? message.authoritativeLifecycleEpoch
      : message.authoritativeLifecycleEpoch;
    const authoritativeLifecycleRevision = active || queued || terminal || advancedBeyondMessage
      ? lifecycleRevision ?? message.authoritativeLifecycleRevision
      : message.authoritativeLifecycleRevision;
    if (
      lifecycle === message.lifecycle &&
      authoritativeQueueObserved === message.authoritativeQueueObserved &&
      authoritativeActiveObserved === message.authoritativeActiveObserved &&
      authoritativeLifecycleEpoch === message.authoritativeLifecycleEpoch &&
      authoritativeLifecycleRevision === message.authoritativeLifecycleRevision
    ) {
      return [message];
    }
    changed = true;
    return [{
      ...message,
      lifecycle,
      authoritativeQueueObserved,
      authoritativeActiveObserved,
      authoritativeLifecycleEpoch,
      authoritativeLifecycleRevision,
    }];
  });

  // Once a confirmed record retires, reserve its provider event ID on every
  // later optimistic record. Otherwise identical queued submissions can
  // reclaim the first message's echo after reconnect or promotion.
  const reserved = retiredConfirmedEventIds.length === 0
    ? reconciled
    : reconciled.map((message) => {
        const previous = new Set(message.createdAfterEventIds ?? []);
        let messageChanged = false;
        for (const id of retiredConfirmedEventIds) {
          if (!previous.has(id)) {
            previous.add(id);
            messageChanged = true;
          }
        }
        if (!messageChanged) {
          return message;
        }
        changed = true;
        return {
          ...message,
          createdAfterEventIds: Array.from(previous),
        };
      });

  reserved.sort((left, right) => {
    const leftQueue = queuedOrder.get(left.turnId);
    const rightQueue = queuedOrder.get(right.turnId);
    if (leftQueue !== undefined && rightQueue !== undefined) {
      return leftQueue - rightQueue;
    }
    if (left.turnId === activeTurnId && rightQueue !== undefined) {
      return -1;
    }
    if (right.turnId === activeTurnId && leftQueue !== undefined) {
      return 1;
    }
    return (originalOrder.get(left.id) ?? 0) - (originalOrder.get(right.id) ?? 0);
  });
  if (
    !changed &&
    reserved.every((message, index) => message === pendingUserMessages[index])
  ) {
    return pendingUserMessages;
  }
  return reserved;
}

export function acknowledgePendingUserMessageWithStructuredTurns<
  T extends PendingUserMessageLifecycleFields,
>(
  message: T,
  acknowledgement: {
    turnId: string;
    lifecycle: PendingUserMessageLifecycle;
    acceptedAt: string;
    turnEpoch?: string;
    turnRevision?: number;
  },
  turn?: StructuredTurn,
  queuedTurns?: StructuredTurn[],
): T {
  const remapped = message.turnId !== acknowledgement.turnId;
  const active = isStructuredTurnRunning(turn) &&
    turn.id === acknowledgement.turnId;
  const queued = Boolean(
    queuedTurns?.some((queuedTurn) => queuedTurn.id === acknowledgement.turnId),
  );
  return {
    ...message,
    turnId: acknowledgement.turnId,
    lifecycle: active ? "sending" : queued ? "queued" : acknowledgement.lifecycle,
    acceptedAt: acknowledgement.acceptedAt,
    authoritativeQueueObserved: remapped
      ? queued || acknowledgement.lifecycle === "queued" || undefined
      : queued || acknowledgement.lifecycle === "queued" ||
        message.authoritativeQueueObserved,
    authoritativeActiveObserved: remapped
      ? active || acknowledgement.lifecycle === "sending" || undefined
      : active || acknowledgement.lifecycle === "sending" ||
        message.authoritativeActiveObserved,
    authoritativeLifecycleEpoch: acknowledgement.turnEpoch ??
      (remapped ? undefined : message.authoritativeLifecycleEpoch),
    authoritativeLifecycleRevision: acknowledgement.turnRevision ??
      (remapped ? undefined : message.authoritativeLifecycleRevision),
  };
}

export function canReconcilePendingAcknowledgementAgainstProjection(
  acknowledgement: { turnEpoch?: string; turnRevision?: number },
  projection: { turn_epoch?: string; turn_revision?: number } | null | undefined,
) {
  return Boolean(
    acknowledgement.turnEpoch &&
      projection?.turn_epoch === acknowledgement.turnEpoch &&
      typeof acknowledgement.turnRevision === "number" &&
      typeof projection.turn_revision === "number" &&
      projection.turn_revision >= acknowledgement.turnRevision,
  );
}

function lifecycleProjectionIsNewer(
  observedEpoch?: string,
  observedRevision?: number,
  projectionEpoch?: string,
  projectionRevision?: number,
) {
  if (!observedEpoch || !projectionEpoch) {
    return false;
  }
  if (observedEpoch !== projectionEpoch) {
    return true;
  }
  return typeof observedRevision === "number" &&
    typeof projectionRevision === "number" &&
    projectionRevision > observedRevision;
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
  return pendingUserMessageLifecycleLabel(
    lifecycle,
    queuedOrdinal,
    queuedCount,
  );
}

export function pendingUserMessageMaxAgeMs(
  lifecycle: PendingUserMessageLifecycle = "sending",
): number {
  if (lifecycle === "settled") {
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
  turn: StructuredTurn | undefined,
  queuedTurns: StructuredTurn[] | undefined,
  now: number,
  orphanLimit: number = 12,
): T[] {
  const authoritativeTurnIds = new Set<string>();
  if (isStructuredTurnRunning(turn)) {
    authoritativeTurnIds.add(turn.id);
  }
  queuedTurns?.forEach((queuedTurn) => {
    authoritativeTurnIds.add(queuedTurn.id);
  });
  const retained = messages.filter(
    (message) =>
      authoritativeTurnIds.has(message.turnId) ||
      Boolean(message.acceptedAt) ||
      !shouldPrunePendingUserMessageByLifecycle(message, now),
  );
  let orphanBudget = Math.max(0, orphanLimit);
  const keep = new Set<string>();
  for (let index = retained.length - 1; index >= 0; index -= 1) {
    const message = retained[index];
    const authoritative = authoritativeTurnIds.has(message.turnId);
    if (authoritative || message.acceptedAt || orphanBudget > 0) {
      keep.add(message.id);
      if (!authoritative && !message.acceptedAt) {
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
      return pendingUserMessageMatchesEcho(message, {
        body: event.body,
        files: event.files,
      });
    });
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

export function pendingUserMessageMatchesEcho(
  message: Pick<
    PendingUserMessageLifecycleFields,
    "body" | "sentText" | "attachments"
  >,
  echo: {
    body?: string;
    attachments?: Array<{ path?: string }>;
    files?: string[];
  },
) {
  const sentText = comparableUserMessageText(message.sentText);
  const body = comparableUserMessageText(message.body);
  const echoText = comparableUserMessageText(echo.body || "");
  const pendingAttachments = comparableAttachmentSignature(
    message.sentText,
    message.attachments,
  );
  const echoAttachments = comparableAttachmentSignature(
    echo.body || "",
    echo.attachments,
    echo.files,
  );

  const textMatches = Boolean(
    echoText &&
      ((sentText && echoText === sentText) || (body && echoText === body)),
  );
  if (textMatches) {
    // Some providers omit attachment metadata from their transcript echo. Keep
    // the established text match in that case, but reject a known mismatch.
    return !pendingAttachments || !echoAttachments ||
      pendingAttachments === echoAttachments;
  }
  return Boolean(
    !sentText &&
      !body &&
      !echoText &&
      pendingAttachments &&
      pendingAttachments === echoAttachments,
  );
}

function comparableAttachmentSignature(
  value: string,
  attachments?: Array<{ path?: string }>,
  files?: string[],
) {
  const taggedPaths = attachmentPathsFromTag(value);
  const paths = taggedPaths.length > 0
    ? taggedPaths
    : (attachments ?? [])
        .map((attachment) => attachment.path?.trim() || "")
        .filter(Boolean);
  const comparablePaths = paths.length > 0
    ? paths
    : (files ?? []).map((path) => path.trim()).filter(Boolean);
  return comparablePaths.join("\u0000");
}

function attachmentPathsFromTag(value: string) {
  const match = ATTACHMENT_TAG_RE.exec(value);
  if (!match) {
    return [];
  }
  try {
    const parsed = JSON.parse(match[1].trim());
    const files = Array.isArray(parsed?.files) ? parsed.files : [];
    return files
      .map((file: unknown) =>
        file && typeof file === "object" &&
          typeof (file as { path?: unknown }).path === "string"
          ? (file as { path: string }).path.trim()
          : "",
      )
      .filter(Boolean);
  } catch {
    return [];
  }
}

export function comparableUserMessageText(value: string) {
  return value
    .replace(ATTACHMENT_TAG_RE, "")
    .replace(/\s+/g, " ")
    .trim();
}
