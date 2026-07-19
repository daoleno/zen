import type { UploadProgressSnapshot } from "../../services/uploads";

export function buildAttachmentUploadPresentation(
  name: string,
  progress: UploadProgressSnapshot | null,
) {
  const progressLabel = buildAttachmentUploadProgressLabel(progress);
  const progressPercent =
    progress?.fraction !== null && progress?.fraction !== undefined
      ? Math.floor(progress.fraction * 100)
      : null;
  return {
    accessibilityLabel: `Uploading ${name}, ${progressLabel}`,
    accessibilityValue:
      progressPercent === null
        ? undefined
        : { min: 0, max: 100, now: progressPercent },
    cancelAccessibilityLabel: `Cancel upload of ${name}`,
    cancelLabel: "Cancel" as const,
    progressLabel,
    progressPercent,
  };
}

export function buildAttachmentUploadProgressLabel(
  progress: UploadProgressSnapshot | null,
) {
  if (!progress) {
    return "Uploading";
  }
  if (progress.fraction !== null && progress.totalBytes !== null) {
    const percent = Math.floor(progress.fraction * 100);
    const transferred = formatAttachmentUploadBytes(
      progress.transferredBytes ?? 0,
    );
    return `${percent}% · ${transferred} / ${formatAttachmentUploadBytes(progress.totalBytes)}`;
  }
  if (progress.transferredBytes !== null) {
    return `Uploading · ${formatAttachmentUploadBytes(progress.transferredBytes)}`;
  }
  return "Uploading";
}

export function formatAttachmentUploadBytes(bytes: number) {
  const value = Math.max(0, bytes);
  const units = ["B", "KB", "MB", "GB", "TB"];
  let unitIndex = 0;
  let scaled = value;
  while (scaled >= 1024 && unitIndex < units.length - 1) {
    scaled /= 1024;
    unitIndex += 1;
  }
  const digits = scaled >= 10 || Number.isInteger(scaled) ? 0 : 1;
  return `${scaled.toFixed(digits)} ${units[unitIndex]}`;
}
