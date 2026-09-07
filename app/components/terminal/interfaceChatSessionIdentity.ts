export function interfaceChatSessionCacheKey(
  serverId: string,
  workerId: string,
  conversationScopeKey?: string,
) {
  return conversationScopeKey
    ? `${serverId}:scope:${conversationScopeKey}`
    : `${serverId}:agent:${workerId}`;
}

export interface InterfaceReadingIdentity {
  owner: string;
  key: string;
  providerId?: string;
}

export function resolveInterfaceReadingIdentity(
  owner: string,
  scopedConversation: boolean,
  providerId: string | undefined,
  previous?: InterfaceReadingIdentity,
): InterfaceReadingIdentity {
  if (scopedConversation) return { owner, key: owner };
  const confirmedId = providerId?.trim() || (previous?.owner === owner ? previous.providerId : undefined);
  return { owner, providerId: confirmedId, key: JSON.stringify([owner, confirmedId ?? null]) };
}
