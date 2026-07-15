export function reconcileComposerStopLatch(
  latchedTurnId: string | undefined,
  workingTurnId: string | undefined,
) {
  return latchedTurnId && latchedTurnId === workingTurnId
    ? latchedTurnId
    : undefined;
}

export function beginComposerStop(
  latchedTurnId: string | undefined,
  workingTurnId: string | undefined,
) {
  if (!workingTurnId || latchedTurnId === workingTurnId) {
    return {
      accepted: false as const,
      latchedTurnId,
    };
  }
  return {
    accepted: true as const,
    latchedTurnId: workingTurnId,
  };
}

export function releaseComposerStopLatch(
  latchedTurnId: string | undefined,
  failedTurnId: string,
) {
  return latchedTurnId === failedTurnId ? undefined : latchedTurnId;
}
