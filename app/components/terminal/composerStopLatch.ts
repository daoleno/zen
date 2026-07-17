export function reconcileComposerStopLatch(
  latchedActivityId: string | undefined,
  runningActivityId: string | undefined,
) {
  return latchedActivityId && latchedActivityId === runningActivityId
    ? latchedActivityId
    : undefined;
}

export function beginComposerStop(
  latchedActivityId: string | undefined,
  runningActivityId: string | undefined,
) {
  if (!runningActivityId || latchedActivityId === runningActivityId) {
    return {
      accepted: false as const,
      latchedActivityId,
    };
  }
  return {
    accepted: true as const,
    latchedActivityId: runningActivityId,
  };
}

export function releaseComposerStopLatch(
  latchedActivityId: string | undefined,
  failedActivityId: string,
) {
  return latchedActivityId === failedActivityId ? undefined : latchedActivityId;
}
