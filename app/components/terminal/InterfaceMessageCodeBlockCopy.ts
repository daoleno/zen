export const CODE_BLOCK_COPIED_RESET_MS = 1_600;

interface CodeBlockCopyFeedbackOptions<Timer> {
  copyText(text: string): Promise<unknown>;
  onCopiedChange(copied: boolean): void;
  scheduleReset(callback: () => void, delayMs: number): Timer;
  cancelReset(timer: Timer): void;
}

export interface CodeBlockCopyFeedback {
  copy(text: string): Promise<void>;
  dispose(): void;
}

export function createCodeBlockCopyFeedback<Timer>({
  copyText,
  onCopiedChange,
  scheduleReset,
  cancelReset,
}: CodeBlockCopyFeedbackOptions<Timer>): CodeBlockCopyFeedback {
  let disposed = false;
  let generation = 0;
  let resetTimer: Timer | null = null;

  const clearReset = () => {
    if (resetTimer === null) {
      return;
    }
    cancelReset(resetTimer);
    resetTimer = null;
  };

  return {
    async copy(text) {
      const copyGeneration = ++generation;
      await copyText(text);
      if (disposed || copyGeneration !== generation) {
        return;
      }

      clearReset();
      onCopiedChange(true);
      resetTimer = scheduleReset(() => {
        resetTimer = null;
        if (disposed || copyGeneration !== generation) {
          return;
        }
        onCopiedChange(false);
      }, CODE_BLOCK_COPIED_RESET_MS);
    },
    dispose() {
      disposed = true;
      generation += 1;
      clearReset();
    },
  };
}
