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
  /** True after one transparent stale-receipt auto-retry with a fresh identity. */
  staleReceiptAutoRetried?: boolean;
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
    staleReceiptAutoRetried?: boolean;
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
    staleReceiptAutoRetried:
      attempt.staleReceiptAutoRetried ?? message.staleReceiptAutoRetried,
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
    // Pending is compact status-mark/a11y only (outboundSentClock), never a
    // bubble outline; bubble keeps busy without a status-only a11y label.
    label: "",
    accessibilityLabel: "Message pending provider transcript",
  };
}

/** True while transport is in-flight; false once failed or acknowledged. */
export function showsPendingSendStatusMark(args: {
  pending?: boolean;
  lifecycle?: PendingUserMessageLifecycle;
}): boolean {
  return Boolean(args.pending) && args.lifecycle !== "failed";
}

/**
 * The stale-receipt auto-retry bound. A `stale_receipt_invalidated` failure
 * means the daemon found a legacy receipt binding for a different payload and
 * proved no provider mutation: the app invalidates that identity and
 * transparently resubmits the same logical input once with a fresh identity.
 * Any later stale failure for the same row is surfaced to the user instead.
 */
export function staleReceiptAutoRetryPolicy(
  message: Pick<
    PendingUserMessageLifecycleFields,
    "staleReceiptAutoRetried"
  >,
  failure: { code?: string },
):
  | { autoRetry: false; reason: "not_stale" | "already_retried" }
  | { autoRetry: true } {
  if (failure.code !== "stale_receipt_invalidated") {
    return { autoRetry: false, reason: "not_stale" };
  }
  if (message.staleReceiptAutoRetried) {
    return { autoRetry: false, reason: "already_retried" };
  }
  return { autoRetry: true };
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
 * Consumes provider/canonical user events against local pending rows.
 *
 * 1. Exact receipt match first (global): user event id === dispatchRequestId.
 *    This is the Brain admission identity and clears Pending without FIFO or
 *    seq-boundary guessing. Message bodies are never identities.
 * 2. Provider-neutral causal FIFO fallback for ordinary provider echoes whose
 *    ids differ from the request receipt.
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

  const consumedEventIds = new Set<string>();
  const clearedPendingIds = new Set<string>();
  const providerEventAliases: ProviderEventTurnFocusAlias[] = [];

  // Pass 1: exact receipt identity, independent of attempt order / seq bounds.
  for (const message of pendingUserMessages) {
    const receiptId = message.dispatchRequestId.trim();
    if (!receiptId || clearedPendingIds.has(message.id)) {
      continue;
    }
    const exact = userEvents.find(
      (event) =>
        event.id === receiptId &&
        !consumedEventIds.has(event.id),
    );
    if (!exact) {
      continue;
    }
    consumedEventIds.add(exact.id);
    clearedPendingIds.add(message.id);
    providerEventAliases.push({
      providerEventId: exact.id,
      localPendingId: message.id,
    });
  }

  const remainingForFifo = pendingUserMessages.filter(
    (message) => !clearedPendingIds.has(message.id),
  );
  if (remainingForFifo.length === 0) {
    return { pendingUserMessages: [], providerEventAliases };
  }

  const attempts = remainingForFifo
    .map((message, presentationIndex) => ({ message, presentationIndex }))
    .sort((left, right) => {
      const byAttempt =
        finiteAttemptOrder(left.message.dispatchAttemptOrder) -
        finiteAttemptOrder(right.message.dispatchAttemptOrder);
      return byAttempt || left.presentationIndex - right.presentationIndex;
    });
  const remainingById = new Map<string, T>();
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
      clearedPendingIds.add(message.id);
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
      if (clearedPendingIds.has(message.id)) {
        return [];
      }
      const remaining = remainingById.get(message.id);
      return remaining ? [remaining] : [];
    }),
    providerEventAliases,
  };
}

function finiteAttemptOrder(value: number) {
  return Number.isFinite(value) ? value : Number.MAX_SAFE_INTEGER;
}
