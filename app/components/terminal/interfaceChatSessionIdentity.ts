export function interfaceChatSessionCacheKey(
  serverId: string,
  workerId: string,
  conversationScopeKey?: string,
) {
  return conversationScopeKey
    ? `${serverId}:scope:${conversationScopeKey}`
    : `${serverId}:agent:${workerId}`;
}
