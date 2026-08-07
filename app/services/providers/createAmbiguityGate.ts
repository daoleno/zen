export type CreateAmbiguityBlock = {
  serverId: string;
  connectionGeneration: number;
  listReceipt: number;
};

export type CreateAmbiguityGateState = Record<string, CreateAmbiguityBlock>;

export function blockCreateAfterAmbiguity(
  state: CreateAmbiguityGateState,
  input: CreateAmbiguityBlock,
): CreateAmbiguityGateState {
  const serverId = input.serverId.trim();
  if (!serverId) return state;
  return {
    ...state,
    [serverId]: {
      serverId,
      connectionGeneration: Math.max(0, input.connectionGeneration),
      listReceipt: Math.max(0, input.listReceipt),
    },
  };
}

export function clearCreateAmbiguityForServer(
  state: CreateAmbiguityGateState,
  serverId: string,
): CreateAmbiguityGateState {
  const id = serverId.trim();
  if (!id || !(id in state)) return state;
  const next = { ...state };
  delete next[id];
  return next;
}

export function shouldUnlockCreateAfterAmbiguity(input: {
  block: CreateAmbiguityBlock | null | undefined;
  connectionGeneration: number;
  listReceipt: number;
  listFreshForConnection: boolean;
}): boolean {
  if (!input.block) return true;
  if (input.listReceipt <= input.block.listReceipt) return false;
  if (input.connectionGeneration === input.block.connectionGeneration) {
    return true;
  }
  return (
    input.connectionGeneration > input.block.connectionGeneration &&
    input.listFreshForConnection
  );
}

export function isCreateBlockedByAmbiguity(input: {
  blocks: CreateAmbiguityGateState;
  serverId: string;
  connectionGeneration: number;
  listReceipt: number;
  listFreshForConnection: boolean;
}): boolean {
  const block = input.blocks[input.serverId.trim()];
  return !shouldUnlockCreateAfterAmbiguity({
    block,
    connectionGeneration: input.connectionGeneration,
    listReceipt: input.listReceipt,
    listFreshForConnection: input.listFreshForConnection,
  });
}

export function bumpAgentSessionListReceipt(
  receipts: Record<string, number>,
  serverId: string,
): Record<string, number> {
  const id = serverId.trim();
  if (!id) return receipts;
  return { ...receipts, [id]: (receipts[id] ?? 0) + 1 };
}
