import type {
  AttachmentUploadOperation,
  UploadedAttachment,
  UploadProgressSnapshot,
} from "./uploads";

export interface CurrentAttachmentUploadHandle {
  readonly generation: number;
  readonly result: Promise<UploadedAttachment>;
}

type ActiveUpload = {
  handle?: CurrentAttachmentUploadHandle;
  operation?: AttachmentUploadOperation;
};

/**
 * A local, single-slot generation guard. Each initiating surface owns one
 * instance; there is no global upload registry or second progress owner.
 */
export class CurrentAttachmentUpload {
  private active: ActiveUpload | null = null;
  private generation = 0;

  start(
    createOperation: (
      onProgress: (progress: UploadProgressSnapshot) => void,
    ) => AttachmentUploadOperation,
    onProgress: (progress: UploadProgressSnapshot) => void,
  ): CurrentAttachmentUploadHandle {
    if (this.active) {
      throw new Error("An attachment upload is already active.");
    }

    const active: ActiveUpload = {};
    this.active = active;
    this.generation += 1;
    try {
      const operation = createOperation((progress) => {
        if (this.active === active) {
          onProgress(progress);
        }
      });
      const handle = {
        generation: this.generation,
        result: operation.result,
      };
      active.handle = handle;
      active.operation = operation;
      return handle;
    } catch (error) {
      if (this.active === active) {
        this.active = null;
      }
      throw error;
    }
  }

  finish(handle: CurrentAttachmentUploadHandle): boolean {
    if (this.active?.handle !== handle) {
      return false;
    }
    this.active = null;
    return true;
  }

  cancel(): Error | null {
    const active = this.active;
    if (!active) {
      return null;
    }

    // Invalidate first so even synchronous late callbacks cannot escape.
    this.active = null;
    this.generation += 1;
    return active.operation?.cancel() ?? null;
  }
}
