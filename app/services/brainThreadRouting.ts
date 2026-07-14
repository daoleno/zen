export function isTargetedBrainThreadReadOnly(
  targetedThreadId?: string,
  liveThreadId?: string,
): boolean {
  if (!targetedThreadId) return false;
  return !liveThreadId || targetedThreadId !== liveThreadId;
}
