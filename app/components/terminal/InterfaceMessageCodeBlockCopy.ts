export const CODE_BLOCK_COPIED_RESET_MS = 1_600;
export const CODE_BLOCK_COPY_TOUCH_SLOP_PX = 10;

export function codeBlockCopyMovedBeyondSlop(
  startX: number,
  startY: number,
  pageX: number,
  pageY: number,
  slop: number = CODE_BLOCK_COPY_TOUCH_SLOP_PX,
): boolean {
  return (
    Math.abs(pageX - startX) > slop || Math.abs(pageY - startY) > slop
  );
}

export function codeBlockCopyShouldCommit(input: {
  gestureActive: boolean;
  userMovedBeyondSlop: boolean;
}): boolean {
  return input.gestureActive && !input.userMovedBeyondSlop;
}

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
  let successGeneration = 0;
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
      try {
        await copyText(text);
      } catch {
        return;
      }
      if (disposed) {
        return;
      }

      const copyGeneration = ++successGeneration;
      clearReset();
      onCopiedChange(true);
      resetTimer = scheduleReset(() => {
        if (disposed || copyGeneration !== successGeneration) {
          return;
        }
        resetTimer = null;
        onCopiedChange(false);
      }, CODE_BLOCK_COPIED_RESET_MS);
    },
    dispose() {
      disposed = true;
      successGeneration += 1;
      clearReset();
    },
  };
}
