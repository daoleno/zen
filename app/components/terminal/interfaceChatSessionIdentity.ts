export function interfaceChatSessionCacheKey(
  serverId: string,
  agentId: string,
  conversationScopeKey?: string,
) {
  return conversationScopeKey
    ? `${serverId}:scope:${conversationScopeKey}`
    : `${serverId}:agent:${agentId}`;
}
