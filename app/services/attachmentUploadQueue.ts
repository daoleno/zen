import type { AttachmentUploadOperation, UploadedAttachment, UploadDocumentAsset, UploadProgressSnapshot } from "./uploads";

export type AttachmentQueueItem = {
  id: string;
  asset: UploadDocumentAsset;
  status: "queued" | "uploading" | "ready" | "failed";
  progress: UploadProgressSnapshot | null;
  result?: UploadedAttachment;
  error?: string;
};

/** One owner, one active native stream; cancellation invalidates callbacks first. */
export class AttachmentUploadQueue {
  private items: AttachmentQueueItem[] = [];
  private active: { item: AttachmentQueueItem; operation?: AttachmentUploadOperation } | null = null;
  private disposed = false;
  constructor(private create: (asset: UploadDocumentAsset, progress: (value: UploadProgressSnapshot) => void) => Promise<AttachmentUploadOperation>, private changed: (items: readonly AttachmentQueueItem[]) => void) {}

  enqueue(items: Array<{ id: string; asset: UploadDocumentAsset }>) {
    if (this.disposed) return;
    this.items.push(...items.map(({ id, asset }): AttachmentQueueItem => ({ id, asset, status: asset.selectionError ? "failed" : "queued", error: asset.selectionError, progress: null })));
    this.publish();
    void this.pump();
  }
  forgetCompleted(retained: ReadonlySet<string>) {
    this.items = this.items.filter((item) => item.status !== "ready" || retained.has(item.id));
  }
  retry(id: string) {
    const item = this.items.find((item) => item.id === id);
    if (!item || item.status !== "failed" || item.asset.selectionRetryable === false || this.disposed) return;
    item.status = "queued";
    item.error = undefined;
    this.publish();
    void this.pump();
  }
  remove(id: string) {
    this.items = this.items.filter((item) => item.id !== id);
    if (this.active?.item.id === id) {
      const active = this.active;
      this.active = null;
      active.operation?.cancel();
    }
    this.publish();
    void this.pump();
  }
  dispose() {
    this.disposed = true;
    const active = this.active;
    this.active = null;
    this.items = [];
    active?.operation?.cancel();
  }
  private publish() { if (!this.disposed) this.changed(this.items.map((item) => ({ ...item }))); }
  private async pump() {
    if (this.disposed || this.active) return;
    const item = this.items.find((item) => item.status === "queued");
    if (!item) return;
    const active: { item: AttachmentQueueItem; operation?: AttachmentUploadOperation } = { item };
    this.active = active;
    item.status = "uploading";
    this.publish();
    try {
      const operation = await this.create(item.asset, (progress) => {
        if (this.active !== active || this.disposed) return;
        item.progress = progress;
        this.publish();
      });
      active.operation = operation;
      if (this.active !== active || this.disposed) { operation.cancel(); void operation.result.catch(() => {}); return; }
      const result = await operation.result;
      if (this.active !== active || this.disposed) return;
      item.result = result;
      item.status = "ready";
    } catch (error) {
      if (this.active !== active || this.disposed) return;
      item.error = error instanceof Error ? error.message : "Could not upload this file.";
      item.status = "failed";
    } finally {
      if (this.active === active) { this.active = null; this.publish(); void this.pump(); }
    }
  }
}
