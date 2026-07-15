import {
  type CodexConversation,
  isStructuredTurnRunning,
  type StructuredTurn,
} from "../../services/codexConversation";
import type { PendingUserMessageLifecycle } from "./pendingUserMessageLifecycle";

export interface PendingStructuredTurn {
  id: string;
  lifecycle: PendingUserMessageLifecycle;
  turnId: string;
  turnStartedAt: string;
  acceptedAt?: string;
}

/**
 * Resolves the single turn that owns both timeline Working and Composer state.
 * A local `sending` record bridges only the interval before the daemon publishes
 * the authoritative turn. Queued messages never masquerade as active work.
 */
export function resolveWorkingStructuredTurn(
  authoritativeTurn: StructuredTurn | undefined,
  pendingUserMessages: PendingStructuredTurn[] = [],
): StructuredTurn | undefined {
  if (isStructuredTurnRunning(authoritativeTurn)) {
    return authoritativeTurn;
  }

  const pending = pendingUserMessages.find(
    (message) => message.lifecycle === "sending" && Boolean(message.acceptedAt),
  );
  if (!pending) {
    return undefined;
  }
  if (authoritativeTurn?.id === pending.turnId) {
    return undefined;
  }
  return {
    id: pending.turnId,
    status: "running",
    started_at: pending.turnStartedAt,
  };
}

export function createStructuredTurnIdentity(
  now: number = Date.now(),
  random: number = Math.random(),
) {
  const startedAt = new Date(now).toISOString();
  return {
    id: `turn:${now.toString(36)}:${random.toString(36).slice(2, 10)}`,
    startedAt,
  };
}

export function codexChatSessionCacheKey(
  serverId: string,
  agentId: string,
  conversationScopeKey?: string,
) {
  return conversationScopeKey
    ? `${serverId}:scope:${conversationScopeKey}`
    : `${serverId}:agent:${agentId}`;
}

export function structuredConversationClientIdentity(
  conversation?: Pick<CodexConversation, "session_id" | "path"> | null,
) {
  if (conversation?.session_id?.trim()) {
    return `session:${conversation.session_id.trim()}`;
  }
  if (conversation?.path?.trim()) {
    return `path:${conversation.path.trim()}`;
  }
  return undefined;
}
