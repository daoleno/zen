export function codexChatSessionCacheKey(
  serverId: string,
  agentId: string,
  conversationScopeKey?: string,
) {
  return conversationScopeKey
    ? `${serverId}:scope:${conversationScopeKey}`
    : `${serverId}:agent:${agentId}`;
}
