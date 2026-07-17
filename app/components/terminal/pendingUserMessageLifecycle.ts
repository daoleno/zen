export const PENDING_MESSAGE_RETRY_ACCESSIBILITY_LABEL =
  "Retry sending message";

export type PendingUserMessageLifecycle = "pending" | "failed";

export type PendingUserMessageLifecycleFields = {
  lifecycle: PendingUserMessageLifecycle;
  dispatchRequestId: string;
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
    createdAfterMaxSeq?: number;
    createdAfterEventIds?: string[];
  },
): T {
  return {
    ...message,
    lifecycle: "pending",
    dispatchRequestId: attempt.requestId,
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

/**
 * Consumes provider user events against local rows in causal FIFO order. Event
 * IDs and sequence boundaries are the only matching inputs; message bodies are
 * intentionally not identities. IDs consumed by an earlier row are folded
 * into the next remaining row's boundary so a later snapshot cannot consume
 * the same provider event twice.
 */
export function reconcilePendingUserMessagesAgainstEvents<
  T extends PendingUserMessageLifecycleFields,
>(pendingUserMessages: T[], events: ReconcileUserEvent[]): T[] {
  if (pendingUserMessages.length === 0 || events.length === 0) {
    return pendingUserMessages;
  }
  const userEvents = events.filter(
    (event) => event.kind === "user_message" && Boolean(event.id),
  );
  if (userEvents.length === 0) {
    return pendingUserMessages;
  }

  const consumedEventIds = new Set<string>();
  const remaining: T[] = [];
  for (const message of pendingUserMessages) {
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
      continue;
    }
    if (consumedEventIds.size === 0) {
      remaining.push(message);
      continue;
    }
    const createdAfterEventIds = Array.from(
      new Set([...priorEventIds, ...consumedEventIds]),
    );
    remaining.push({
      ...message,
      createdAfterEventIds,
    });
  }
  return remaining;
}
