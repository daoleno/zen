export type TurnElapsedEvent = {
  kind: string;
  timestamp?: string;
};

export type TurnElapsedPendingMessage = {
  createdAt: string;
};

export function resolveWorkingTurnStartedAt({
  events,
  pendingUserMessages = [],
}: {
  events: TurnElapsedEvent[];
  pendingUserMessages?: TurnElapsedPendingMessage[];
}): string | undefined {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    if (event?.kind !== "user_message" || !event.timestamp) {
      continue;
    }
    const timestamp = new Date(event.timestamp).getTime();
    if (Number.isFinite(timestamp)) {
      return event.timestamp;
    }
  }

  let earliestMs = Number.POSITIVE_INFINITY;
  let earliestIso: string | undefined;
  for (const message of pendingUserMessages) {
    const timestamp = new Date(message.createdAt).getTime();
    if (!Number.isFinite(timestamp) || timestamp >= earliestMs) {
      continue;
    }
    earliestMs = timestamp;
    earliestIso = message.createdAt;
  }
  return earliestIso;
}

export function elapsedSecondsSince(
  startedAt: string | undefined,
  nowMs: number,
): number | null {
  if (!startedAt) {
    return null;
  }
  const timestamp = new Date(startedAt).getTime();
  if (!Number.isFinite(timestamp)) {
    return null;
  }
  return Math.max(0, Math.floor((nowMs - timestamp) / 1000));
}

export function formatElapsedDuration(totalSeconds: number): string {
  const seconds = Math.max(0, totalSeconds);
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const remainder = seconds % 60;
  const paddedSeconds = remainder.toString().padStart(2, "0");

  if (hours > 0) {
    return `${hours}h ${minutes.toString().padStart(2, "0")}m ${paddedSeconds}s`;
  }
  if (minutes > 0) {
    return `${minutes}m ${paddedSeconds}s`;
  }
  return `${remainder}s`;
}

export function workingTurnElapsedLabel({
  startedAt,
  nowMs,
  active,
}: {
  startedAt?: string;
  nowMs: number;
  active: boolean;
}): string {
  if (!active) {
    return "";
  }
  const elapsed = elapsedSecondsSince(startedAt, nowMs);
  if (elapsed === null) {
    return "";
  }
  return formatElapsedDuration(elapsed);
}
