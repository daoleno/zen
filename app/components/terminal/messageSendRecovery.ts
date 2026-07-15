export function restoreFailedDraft(previousDraft: string, currentDraft: string) {
  if (!currentDraft.trim()) {
    return previousDraft;
  }
  if (!previousDraft.trim() || currentDraft === previousDraft) {
    return currentDraft;
  }
  return `${previousDraft}\n${currentDraft}`;
}

export function restoreFailedAttachments<T extends { id: string }>(
  previous: T[],
  current: T[],
) {
  const seen = new Set(current.map((attachment) => attachment.id));
  return [
    ...previous.filter((attachment) => !seen.has(attachment.id)),
    ...current,
  ];
}
