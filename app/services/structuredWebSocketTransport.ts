const WEB_SOCKET_OPEN_STATE = 1;

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
  };
}
