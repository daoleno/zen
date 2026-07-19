export const PENDING_MESSAGE_RETRY_ACCESSIBILITY_LABEL =
  "Retry sending message";

export type PendingUserMessageLifecycle = "pending" | "failed";

export type PendingUserMessageLifecycleFields = {
  lifecycle: PendingUserMessageLifecycle;
  dispatchRequestId: string;
  dispatchAttemptOrder: number;
  failureCode?: string;
  failureMessage?: string;
  createdAfterMaxSeq?: number;
  createdAfterEventIds?: string[];
};

export function beginPendingUserMessageAttempt<
  T extends PendingUserMessageLifecycleFields,
>(
  message: T,
  attempt: {
    requestId: string;
    dispatchAttemptOrder: number;
    createdAfterMaxSeq?: number;
    createdAfterEventIds?: string[];
  },
): T {
  return {
    ...message,
    lifecycle: "pending",
    dispatchRequestId: attempt.requestId,
    dispatchAttemptOrder: attempt.dispatchAttemptOrder,
    failureCode: undefined,
    failureMessage: undefined,
    createdAfterMaxSeq: attempt.createdAfterMaxSeq,
    createdAfterEventIds: attempt.createdAfterEventIds,
  };
}

export function rejectPendingUserMessage<
  T extends PendingUserMessageLifecycleFields,
>(
  message: T,
  failure: {
    requestId: string;
    code: string;
    message: string;
  },
): T {
  if (message.dispatchRequestId !== failure.requestId) {
    return message;
  }
  return {
    ...message,
    lifecycle: "failed",
    failureCode: failure.code,
    failureMessage: failure.message,
  };
}

export type PendingLifecyclePresentation = {
  lifecycle: PendingUserMessageLifecycle;
  label: string;
  accessibilityLabel: string;
};

export function presentPendingUserMessageLifecycle(
  message: Pick<PendingUserMessageLifecycleFields, "lifecycle">,
): PendingLifecyclePresentation {
  if (message.lifecycle === "failed") {
    return {
      lifecycle: "failed",
      label: "Send failed",
      accessibilityLabel: "Message send failed",
    };
  }
  return {
    lifecycle: "pending",
    label: "Pending",
    accessibilityLabel: "Message pending provider transcript",
  };
}

export type ReconcileUserEvent = {
  id: string;
  seq?: number;
  kind: string;
};

export type ProviderEventTurnFocusAlias = {
  providerEventId: string;
  localPendingId: string;
};

export type PendingUserMessageReconciliation<T> = {
  pendingUserMessages: T[];
  providerEventAliases: ProviderEventTurnFocusAlias[];
};

/**
 * Consumes provider user events against successful dispatch attempts in their
 * process-local order. The persistent UI array is never reordered. Event IDs
 * and sequence boundaries are the only matching inputs; message bodies are
 * intentionally not identities. IDs consumed by an earlier attempt are folded
 * into the next remaining attempt's boundary so a later snapshot cannot
 * consume the same provider event twice.
 */
export function reconcilePendingUserMessagesAgainstEvents<
  T extends PendingUserMessageLifecycleFields & { id: string },
>(
  pendingUserMessages: T[],
  events: ReconcileUserEvent[],
): PendingUserMessageReconciliation<T> {
  if (pendingUserMessages.length === 0 || events.length === 0) {
    return { pendingUserMessages, providerEventAliases: [] };
  }
  const userEvents = events.filter(
    (event) => event.kind === "user_message" && Boolean(event.id),
  );
  if (userEvents.length === 0) {
    return { pendingUserMessages, providerEventAliases: [] };
  }

  const attempts = pendingUserMessages
    .map((message, presentationIndex) => ({ message, presentationIndex }))
    .sort((left, right) => {
      const byAttempt =
        finiteAttemptOrder(left.message.dispatchAttemptOrder) -
        finiteAttemptOrder(right.message.dispatchAttemptOrder);
      return byAttempt || left.presentationIndex - right.presentationIndex;
    });
  const consumedEventIds = new Set<string>();
  const remainingById = new Map<string, T>();
  const providerEventAliases: ProviderEventTurnFocusAlias[] = [];
  for (const { message } of attempts) {
    const priorEventIds = new Set(message.createdAfterEventIds ?? []);
    const providerEvent = userEvents.find((event) => {
      if (priorEventIds.has(event.id) || consumedEventIds.has(event.id)) {
        return false;
      }
      return !(
        typeof message.createdAfterMaxSeq === "number" &&
        Number.isFinite(message.createdAfterMaxSeq) &&
        typeof event.seq === "number" &&
        event.seq <= message.createdAfterMaxSeq
      );
    });
    if (providerEvent) {
      consumedEventIds.add(providerEvent.id);
      providerEventAliases.push({
        providerEventId: providerEvent.id,
        localPendingId: message.id,
      });
      continue;
    }
    if (consumedEventIds.size === 0) {
      remainingById.set(message.id, message);
      continue;
    }
    const createdAfterEventIds = Array.from(
      new Set([...priorEventIds, ...consumedEventIds]),
    );
    remainingById.set(message.id, {
      ...message,
      createdAfterEventIds,
    });
  }
  return {
    pendingUserMessages: pendingUserMessages.flatMap((message) => {
      const remaining = remainingById.get(message.id);
      return remaining ? [remaining] : [];
    }),
    providerEventAliases,
  };
}

function finiteAttemptOrder(value: number) {
  return Number.isFinite(value) ? value : Number.MAX_SAFE_INTEGER;
}
