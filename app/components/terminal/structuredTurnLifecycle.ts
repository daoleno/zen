import {
  type CodexConversation,
  isStructuredTurnRunning,
  type StructuredTurn,
} from "../../services/codexConversation";

/**
 * Resolves the single Activity that owns timeline Working and Composer state.
 * Submission acceptance is never executor lifecycle evidence.
 */
export function resolveWorkingStructuredTurn(
  authoritativeTurn: StructuredTurn | undefined,
): StructuredTurn | undefined {
  return isStructuredTurnRunning(authoritativeTurn)
    ? authoritativeTurn
    : undefined;
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
  conversation?: Pick<CodexConversation, "session_id" | "path" | "cwd"> | null,
) {
  if (conversation?.session_id?.trim()) {
    return `session:${conversation.session_id.trim()}`;
  }
  if (conversation?.path?.trim()) {
    return `path:${conversation.path.trim()}`;
  }
  return undefined;
}
