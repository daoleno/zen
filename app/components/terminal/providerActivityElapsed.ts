/** Formats elapsed time from the provider-owned Activity start timestamp. */
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

export function formatComposerElapsedDuration(totalSeconds: number): string {
  const seconds = Math.max(0, Math.floor(totalSeconds));
  if (seconds < 60) {
    return `${seconds}s`;
  }
  if (seconds < 3600) {
    const minutes = Math.floor(seconds / 60);
    return `${minutes}:${(seconds % 60).toString().padStart(2, "0")}`;
  }
  const hours = Math.floor(seconds / 3600);
  if (hours >= 100) {
    return "99h+";
  }
  const minutes = Math.floor((seconds % 3600) / 60);
  return `${hours}h${minutes.toString().padStart(2, "0")}`;
}

export function elapsedNowForRender(
  sampledNow: number,
  renderNow: number,
  active: boolean,
) {
  return active ? renderNow : sampledNow;
}

export function providerActivityElapsedLabel({
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

export function providerActivityElapsedLabels(input: {
  startedAt?: string;
  nowMs: number;
  active: boolean;
}) {
  const elapsed = input.active
    ? elapsedSecondsSince(input.startedAt, input.nowMs)
    : null;
  return elapsed === null
    ? { visual: "", accessibility: "" }
    : {
        visual: formatComposerElapsedDuration(elapsed),
        accessibility: formatElapsedDuration(elapsed),
      };
}
