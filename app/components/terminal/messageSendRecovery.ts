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

export function beginLiveMessageAttempt<
  Receipt,
  PendingMessageID,
  Attachment extends { id: string },
>({
  writeNow,
  createOptimisticRow,
  previousDraft,
  currentDraft,
  previousAttachments,
  currentAttachments,
}: {
  writeNow(): Receipt;
  createOptimisticRow(receipt: Receipt): PendingMessageID;
  previousDraft: string;
  currentDraft: string;
  previousAttachments: Attachment[];
  currentAttachments: Attachment[];
}):
  | {
      kind: "written";
      receipt: Receipt;
      pendingMessageId: PendingMessageID;
    }
  | {
      kind: "write_failed";
      error: unknown;
      restoredDraft: string;
      restoredAttachments: Attachment[];
    } {
  let receipt: Receipt;
  try {
    receipt = writeNow();
  } catch (error) {
    return {
      kind: "write_failed",
      error,
      restoredDraft: restoreFailedDraft(previousDraft, currentDraft),
      restoredAttachments: restoreFailedAttachments(
        previousAttachments,
        currentAttachments,
      ),
    };
  }

  return {
    kind: "written",
    receipt,
    pendingMessageId: createOptimisticRow(receipt),
  };
}
