import { describe, expect, test } from "bun:test";
import type {
  AttachmentUploadOperation,
  UploadedAttachment,
  UploadProgressSnapshot,
} from "./uploads";
import { CurrentAttachmentUpload } from "./currentAttachmentUpload";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((nextResolve, nextReject) => {
    resolve = nextResolve;
    reject = nextReject;
  });
  return { promise, reject, resolve };
}

function fakeOperation(
  result: Promise<UploadedAttachment>,
  cancel: () => Error | null = () => null,
): AttachmentUploadOperation {
  return { result, cancel };
}

const attachment: UploadedAttachment = {
  name: "archive.zip",
  path: "/state/uploads/archive.zip",
};

describe("Interface attachment upload ownership", () => {
  test("cancel invalidates progress and late success before attachment mutation", async () => {
    const completion = deferred<UploadedAttachment>();
    const owner = new CurrentAttachmentUpload();
    const progress: UploadProgressSnapshot[] = [];
    let emitProgress!: (value: UploadProgressSnapshot) => void;
    let cancelCalls = 0;
    const handle = owner.start(
      (onProgress) => {
        emitProgress = onProgress;
        return fakeOperation(completion.promise, () => {
          cancelCalls += 1;
          return null;
        });
      },
      (value) => progress.push(value),
    );
    const attached: UploadedAttachment[] = [];

    emitProgress({ transferredBytes: 10, totalBytes: 100, fraction: 0.1 });
    expect(owner.cancel()).toBeNull();
    emitProgress({ transferredBytes: 90, totalBytes: 100, fraction: 0.9 });
    completion.resolve(attachment);
    const result = await handle.result;
    if (owner.finish(handle)) {
      attached.push(result);
    }

    expect(progress).toEqual([
      { transferredBytes: 10, totalBytes: 100, fraction: 0.1 },
    ]);
    expect(attached).toEqual([]);
    expect(cancelCalls).toBe(1);
    expect(owner.cancel()).toBeNull();
    expect(cancelCalls).toBe(1);
  });

  test("a fresh upload can succeed after cancellation", async () => {
    const owner = new CurrentAttachmentUpload();
    const first = deferred<UploadedAttachment>();
    let firstCancelCalls = 0;
    const firstHandle = owner.start(
      () =>
        fakeOperation(first.promise, () => {
          firstCancelCalls += 1;
          return null;
        }),
      () => {},
    );

    owner.cancel();
    first.resolve(attachment);
    await firstHandle.result;
    expect(owner.finish(firstHandle)).toBe(false);

    const secondHandle = owner.start(
      () =>
        fakeOperation(
          Promise.resolve({
            ...attachment,
            path: "/state/uploads/retry.zip",
          }),
        ),
      () => {},
    );
    const retry = await secondHandle.result;

    expect(owner.finish(secondHandle)).toBe(true);
    expect(retry.path).toBe("/state/uploads/retry.zip");
    expect(firstCancelCalls).toBe(1);
  });
});

describe("Terminal attachment upload ownership", () => {
  test("cancel wins a cancel-vs-success race and never injects a PTY path", async () => {
    const completion = deferred<UploadedAttachment>();
    const owner = new CurrentAttachmentUpload();
    let cancelCalls = 0;
    const handle = owner.start(
      () =>
        fakeOperation(completion.promise, () => {
          cancelCalls += 1;
          return null;
        }),
      () => {},
    );
    const ptyInput: string[] = [];

    completion.resolve(attachment);
    owner.cancel();
    const result = await handle.result;
    if (owner.finish(handle)) {
      ptyInput.push(result.path);
    }

    expect(ptyInput).toEqual([]);
    expect(cancelCalls).toBe(1);
  });

  test("replacement/unmount invalidation cancels the exact current operation once", () => {
    const owner = new CurrentAttachmentUpload();
    let firstCancelCalls = 0;
    let secondCancelCalls = 0;

    owner.start(
      () =>
        fakeOperation(new Promise(() => {}), () => {
          firstCancelCalls += 1;
          return null;
        }),
      () => {},
    );
    owner.cancel();
    owner.start(
      () =>
        fakeOperation(new Promise(() => {}), () => {
          secondCancelCalls += 1;
          return null;
        }),
      () => {},
    );
    owner.cancel();
    owner.cancel();

    expect(firstCancelCalls).toBe(1);
    expect(secondCancelCalls).toBe(1);
  });

  test("starting a second in-flight operation is rejected", () => {
    const owner = new CurrentAttachmentUpload();
    owner.start(
      () => fakeOperation(new Promise(() => {})),
      () => {},
    );

    expect(() =>
      owner.start(
        () => fakeOperation(new Promise(() => {})),
        () => {},
      ),
    ).toThrow("An attachment upload is already active.");
  });
});
