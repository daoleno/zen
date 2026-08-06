/**
 * Presentation-only coalescer for streaming Markdown bodies.
 *
 * Provider/reconciliation remain lossless outside this owner. During
 * streaming=true, at most one newest value is published per injected animation
 * frame. streaming=false is a synchronous terminal flush that invalidates every
 * older scheduled callback. dispose() cancels owned frame work permanently.
 */

export type AnimationFrameHandle = number | object;

export type AnimationFrameScheduler = {
  request(callback: () => void): AnimationFrameHandle;
  cancel(handle: AnimationFrameHandle): void;
};

export type StreamingMarkdownFrameControllerOptions = {
  scheduler: AnimationFrameScheduler;
  onPublish(value: string): void;
};

export type StreamingMarkdownFrameController = {
  /**
   * Accept the latest body for this message-row presentation owner.
   * Streaming revisions coalesce; terminal (streaming=false) flushes exactly
   * and synchronously.
   */
  accept(value: string, streaming: boolean): void;
  dispose(): void;
};

export function createStreamingMarkdownFrameController(
  options: StreamingMarkdownFrameControllerOptions,
): StreamingMarkdownFrameController {
  let disposed = false;
  let generation = 0;
  let handle: AnimationFrameHandle | null = null;
  let pending: string | null = null;

  const cancelScheduled = () => {
    if (handle === null) {
      return;
    }
    options.scheduler.cancel(handle);
    handle = null;
  };

  const invalidateScheduled = () => {
    generation += 1;
    cancelScheduled();
    pending = null;
  };

  return {
    accept(value: string, streaming: boolean) {
      if (disposed) {
        return;
      }
      if (!streaming) {
        invalidateScheduled();
        options.onPublish(value);
        return;
      }
      pending = value;
      if (handle !== null) {
        return;
      }
      const scheduledGeneration = generation;
      handle = options.scheduler.request(() => {
        if (disposed || scheduledGeneration !== generation) {
          return;
        }
        handle = null;
        const next = pending;
        pending = null;
        if (next === null) {
          return;
        }
        options.onPublish(next);
      });
    },
    dispose() {
      if (disposed) {
        return;
      }
      disposed = true;
      invalidateScheduled();
    },
  };
}
