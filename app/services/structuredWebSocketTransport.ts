const WEB_SOCKET_OPEN_STATE = 1;

export type StructuredCommandRejection = {
  requestId: string;
  code: string;
  message: string;
};

export type StructuredCommandOutcome<T> =
  | { kind: "confirmed"; value: T }
  | { kind: "unconfirmed" }
  | { kind: "rejected"; rejection: StructuredCommandRejection };

export type StructuredCommandReceipt<T> = {
  requestId: string;
  outcome: Promise<StructuredCommandOutcome<T>>;
};

type StructuredCommandEventSource = {
  on(type: string, handler: (payload: any) => void): void;
  off(type: string, handler: (payload: any) => void): void;
};

/**
 * Observes an acknowledgement without making it the disposition of the
 * socket write. A successful sendNow returns a receipt immediately; timeout,
 * disconnect, or a generic correlated error can only make delivery
 * unconfirmed. Only the operation-specific rejection event is authoritative.
 */
export function dispatchStructuredCommand<T>({
  requestId,
  eventSource,
  confirmedType,
  rejectedType,
  matches,
  normalizeConfirmed,
  sendNow,
  timeoutMs = 10_000,
}: {
  requestId: string;
  eventSource: StructuredCommandEventSource;
  confirmedType: string;
  rejectedType?: string;
  matches(payload: any): boolean;
  normalizeConfirmed(payload: any): T;
  sendNow(): void;
  timeoutMs?: number;
}): StructuredCommandReceipt<T> {
  let settled = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let resolveOutcome!: (outcome: StructuredCommandOutcome<T>) => void;
  const outcome = new Promise<StructuredCommandOutcome<T>>((resolve) => {
    resolveOutcome = resolve;
  });
  const cleanup = () => {
    if (timer) {
      clearTimeout(timer);
      timer = undefined;
    }
    eventSource.off(confirmedType, handleConfirmed);
    if (rejectedType) {
      eventSource.off(rejectedType, handleRejected);
    }
    eventSource.off("input_unconfirmed", handleUnconfirmed);
    eventSource.off("error", handleUnconfirmed);
  };
  const finish = (next: StructuredCommandOutcome<T>) => {
    if (settled) {
      return;
    }
    settled = true;
    cleanup();
    resolveOutcome(next);
  };
  function handleConfirmed(payload: any) {
    if (matches(payload)) {
      finish({ kind: "confirmed", value: normalizeConfirmed(payload) });
    }
  }
  function handleRejected(payload: any) {
    if (!matches(payload)) {
      return;
    }
    finish({
      kind: "rejected",
      rejection: {
        requestId,
        code:
          typeof payload?.code === "string" && payload.code
            ? payload.code
            : "command_rejected",
        message:
          typeof payload?.message === "string" && payload.message
            ? payload.message
            : "The daemon did not accept this command.",
      },
    });
  }
  function handleUnconfirmed(payload: any) {
    if (matches(payload)) {
      finish({ kind: "unconfirmed" });
    }
  }

  eventSource.on(confirmedType, handleConfirmed);
  if (rejectedType) {
    eventSource.on(rejectedType, handleRejected);
  }
  eventSource.on("input_unconfirmed", handleUnconfirmed);
  eventSource.on("error", handleUnconfirmed);
  timer = setTimeout(() => finish({ kind: "unconfirmed" }), timeoutMs);
  try {
    sendNow();
  } catch (error) {
    settled = true;
    cleanup();
    throw error;
  }
  return { requestId, outcome };
}

/** Structured provider commands are never queued in the app transport. */
export function sendWebSocketMessageNow(
  socket: Pick<WebSocket, "readyState" | "send"> | null,
  message: object,
) {
  if (!socket || socket.readyState !== WEB_SOCKET_OPEN_STATE) {
    throw new Error("Daemon is not connected.");
  }
  socket.send(JSON.stringify(message));
}

export function structuredInputMessage(input: {
  requestId: string;
  agentId: string;
  text: string;
  conversationScopeKey?: string;
  turnId?: string;
  turnStartedAt?: string;
  turnQueued?: boolean;
  turnConversationIdentity?: string;
}) {
  return {
    type: "send_input",
    request_id: input.requestId,
    agent_id: input.agentId,
    text: input.text,
    conversation_scope_key: input.conversationScopeKey,
    turn_id: input.turnId,
    turn_started_at: input.turnStartedAt,
    turn_queued: input.turnQueued,
    turn_conversation_identity: input.turnConversationIdentity,
  };
}

export function structuredActionMessage(input: {
  requestId: string;
  agentId: string;
  action: string;
  conversationScopeKey?: string;
  turnId?: string;
  turnStartedAt?: string;
}) {
  return {
    type: "send_action",
    request_id: input.requestId,
    agent_id: input.agentId,
    action: input.action,
    conversation_scope_key: input.conversationScopeKey,
    turn_id: input.turnId,
    turn_started_at: input.turnStartedAt,
  };
}

export function normalizeStructuredInputAccepted(
  payload: any,
  fallbackTurnId?: string,
) {
  return {
    turnId:
      typeof payload?.turn_id === "string" && payload.turn_id
        ? payload.turn_id
        : fallbackTurnId,
    queued: payload?.queued === true,
    turnEpoch:
      typeof payload?.turn_epoch === "string" && payload.turn_epoch
        ? payload.turn_epoch
        : undefined,
    turnRevision:
      typeof payload?.turn_revision === "number" &&
        Number.isFinite(payload.turn_revision)
        ? payload.turn_revision
        : undefined,
    position:
      typeof payload?.position === "number" && Number.isFinite(payload.position)
        ? payload.position
        : undefined,
    conversationRevision:
      typeof payload?.conversation_revision === "number" &&
        Number.isFinite(payload.conversation_revision)
        ? payload.conversation_revision
        : undefined,
  };
}
