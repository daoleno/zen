const WEB_SOCKET_OPEN_STATE = 1;

export type StructuredCommandFailure = {
  requestId: string;
  code: string;
  message: string;
};

export type StructuredCommandOutcome =
  | { kind: "sent" }
  | { kind: "pending" }
  | { kind: "failed"; failure: StructuredCommandFailure }
  | { kind: "connection_closed" };

export type StructuredCommandReceipt = {
  requestId: string;
  outcome: Promise<StructuredCommandOutcome>;
};

type StructuredCommandEventSource = {
  on(type: string, handler: (payload: any) => void): void;
  off(type: string, handler: (payload: any) => void): void;
};

/**
 * Writes one live command and observes only its immediate send result. The
 * daemon ACK means the provider call returned successfully; it is not a
 * provider lifecycle signal. A closed connection only releases listeners.
 */
export function dispatchStructuredCommand({
  requestId,
  eventSource,
  sentType,
  failedType,
  pendingType,
  matches,
  matchesConnection,
  sendNow,
}: {
  requestId: string;
  eventSource: StructuredCommandEventSource;
  sentType: string;
  failedType: string;
  pendingType?: string;
  matches(payload: any): boolean;
  matchesConnection(payload: any): boolean;
  sendNow(): void;
}): StructuredCommandReceipt {
  let settled = false;
  let resolveOutcome!: (outcome: StructuredCommandOutcome) => void;
  const outcome = new Promise<StructuredCommandOutcome>((resolve) => {
    resolveOutcome = resolve;
  });
  const cleanup = () => {
    eventSource.off(sentType, handleSent);
    eventSource.off(failedType, handleFailed);
    if (pendingType) {
      eventSource.off(pendingType, handlePending);
    }
    eventSource.off("disconnected", handleDisconnected);
  };
  const finish = (next: StructuredCommandOutcome) => {
    if (settled) {
      return;
    }
    settled = true;
    cleanup();
    resolveOutcome(next);
  };
  function handleSent(payload: any) {
    if (matches(payload)) {
      finish({ kind: "sent" });
    }
  }
  function handleFailed(payload: any) {
    if (!matches(payload)) {
      return;
    }
    finish({
      kind: "failed",
      failure: {
        requestId,
        code:
          typeof payload?.code === "string" && payload.code
            ? payload.code
            : "command_failed",
        message:
          typeof payload?.message === "string" && payload.message
            ? payload.message
            : "The provider command failed.",
      },
    });
  }
  function handlePending(payload: any) {
    if (matches(payload)) {
      finish({ kind: "pending" });
    }
  }
  function handleDisconnected(payload: any) {
    if (matchesConnection(payload)) {
      finish({ kind: "connection_closed" });
    }
  }

  eventSource.on(sentType, handleSent);
  eventSource.on(failedType, handleFailed);
  if (pendingType) {
    eventSource.on(pendingType, handlePending);
  }
  eventSource.on("disconnected", handleDisconnected);
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
}) {
  return {
    type: "send_input",
    request_id: input.requestId,
    agent_id: input.agentId,
    text: input.text,
  };
}

export function structuredActionMessage(input: {
  requestId: string;
  agentId: string;
  action: string;
}) {
  return {
    type: "send_action",
    request_id: input.requestId,
    agent_id: input.agentId,
    action: input.action,
  };
}
