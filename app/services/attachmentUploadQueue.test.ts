import { expect, test } from "bun:test";
import { AttachmentUploadQueue, type AttachmentQueueItem } from "./attachmentUploadQueue";
import type { AttachmentUploadOperation, UploadedAttachment } from "./uploads";

const tick = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };
function deferred() {
  let resolve!: (value: UploadedAttachment) => void;
  let reject!: (error: Error) => void;
  const result = new Promise<UploadedAttachment>((yes, no) => { resolve = yes; reject = no; });
  let cancelled = false;
  const operation: AttachmentUploadOperation = { result, cancel() { cancelled = true; reject(new Error("cancelled")); return null; } };
  return { operation, resolve, reject, get cancelled() { return cancelled; } };
}
const entries = ["a", "b", "c"].map((id) => ({ id, asset: { uri: `content://fixture/${id}`, name: id } }));

test("sequential queue preserves order and successes across one failure and retry", async () => {
  const calls: string[] = [];
  const operations = [deferred(), deferred(), deferred(), deferred()];
  let snapshot: readonly AttachmentQueueItem[] = [];
  const queue = new AttachmentUploadQueue(async (asset) => { calls.push(asset.name); return operations[calls.length - 1].operation; }, (items) => { snapshot = items; });
  queue.enqueue(entries);
  await tick();
  expect(calls).toEqual(["a"]);
  operations[0].resolve({ path: "/a", name: "a" }); await tick();
  operations[1].reject(new Error("offline")); await tick();
  operations[2].resolve({ path: "/c", name: "c" }); await tick();
  expect(snapshot.map((item) => [item.id, item.status])).toEqual([["a", "ready"], ["b", "failed"], ["c", "ready"]]);
  queue.retry("b"); await tick();
  operations[3].resolve({ path: "/b", name: "b" }); await tick();
  expect(calls).toEqual(["a", "b", "c", "b"]);
  expect(snapshot.map((item) => item.result?.path)).toEqual(["/a", "/b", "/c"]);
});

test("owner disposal cancels active stream and ignores late creation and queued work", async () => {
  const operation = deferred();
  let release!: (value: AttachmentUploadOperation) => void;
  let publications = 0;
  const queue = new AttachmentUploadQueue(() => new Promise((resolve) => { release = resolve; }), () => publications++);
  queue.enqueue(entries);
  queue.dispose();
  const atDispose = publications;
  release(operation.operation);
  await tick();
  expect(operation.cancelled).toBe(true);
  expect(publications).toBe(atDispose);
});
