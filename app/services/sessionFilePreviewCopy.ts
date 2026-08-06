export const SESSION_FILE_COPIED_RESET_MS = 1_600;

interface SessionFileCopyFeedbackOptions<Timer> {
  copyText(text: string): Promise<unknown>;
  onCopiedChange(copied: boolean): void;
  scheduleReset(callback: () => void, delayMs: number): Timer;
  cancelReset(timer: Timer): void;
  resetDelayMs?: number;
}

export interface SessionFileCopyFeedback {
  copy(text: string): Promise<void>;
  dispose(): void;
}

/**
 * Copy → Check feedback for Session file preview.
 * Success is shown only after the clipboard write resolves; failures stay quiet.
 */
export function createSessionFileCopyFeedback<Timer>({
  copyText,
  onCopiedChange,
  scheduleReset,
  cancelReset,
  resetDelayMs = SESSION_FILE_COPIED_RESET_MS,
}: SessionFileCopyFeedbackOptions<Timer>): SessionFileCopyFeedback {
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
      }, resetDelayMs);
    },
    dispose() {
      disposed = true;
      successGeneration += 1;
      clearReset();
    },
  };
}

export interface SessionFileCopyLifecycleOwner {
  /**
   * Dispose the current controller and install a fresh one.
   * Call when the previewed file or request epoch changes.
   */
  replaceController(): void;
  copy(text: string): Promise<void>;
  dispose(): void;
}

/**
 * Owns copy feedback across file/request changes.
 * Disposing a controller permanently must never leave the sheet on a dead instance;
 * file switches call `replaceController()` to create a live controller again.
 */
export function createSessionFileCopyLifecycleOwner<Timer>(
  options: SessionFileCopyFeedbackOptions<Timer>,
): SessionFileCopyLifecycleOwner {
  let ownerDisposed = false;
  let controller: SessionFileCopyFeedback | null = null;

  const install = (clearCopied: boolean) => {
    controller?.dispose();
    controller = null;
    if (clearCopied) {
      options.onCopiedChange(false);
    }
    if (ownerDisposed) {
      return;
    }
    controller = createSessionFileCopyFeedback(options);
  };

  install(false);

  return {
    replaceController() {
      install(true);
    },
    async copy(text) {
      if (ownerDisposed) {
        return;
      }
      if (!controller) {
        install(false);
      }
      await controller!.copy(text);
    },
    dispose() {
      ownerDisposed = true;
      controller?.dispose();
      controller = null;
      options.onCopiedChange(false);
    },
  };
}
