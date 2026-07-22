import { describe, expect, test } from "bun:test";
import {
  createSessionPdfStage,
  SessionPdfStageError,
  sessionPdfStageFileName,
  type SessionPdfDownloadOptions,
  type SessionPdfStagingBackend,
  type SessionPdfStagingFile,
} from "./sessionPdfStaging";

class FakeFile implements SessionPdfStagingFile {
  exists = false;
  size: number | null = null;
  deleteCount = 0;

  constructor(readonly uri: string) {}

  delete() {
    this.exists = false;
    this.deleteCount += 1;
  }
}

function backendWithDownload(
  download: (
    target: FakeFile,
    options: SessionPdfDownloadOptions,
  ) => Promise<SessionPdfStagingFile>,
) {
  const targets: FakeFile[] = [];
  const backend: SessionPdfStagingBackend = {
    createTarget(name) {
      const target = new FakeFile(`file:///cache/${name}`);
      targets.push(target);
      return target;
    },
    download(_uri, target, options) {
      return download(target as FakeFile, options);
    },
  };
  return { backend, targets };
}

function stageInput(generation = "generation-a", epoch = 1) {
  return {
    uri: "https://server.example/session-file",
    headers: { Authorization: "ZenDevice signed" },
    generation,
    expectedBytes: 128,
    owner: "sheet-owner",
    epoch,
  };
}

describe("Session PDF staging", () => {
  test("keeps one authenticated staged file only for the operation lifetime", async () => {
    const receivedHeaders: Record<string, string>[] = [];
    const { backend, targets } = backendWithDownload(
      async (target, options) => {
        receivedHeaders.push(options.headers);
        target.exists = true;
        target.size = 128;
        return target;
      },
    );
    const operation = createSessionPdfStage(stageInput(), backend);

    expect(await operation.result).toBe(targets[0].uri);
    expect(receivedHeaders).toEqual([{ Authorization: "ZenDevice signed" }]);
    expect(targets[0].exists).toBe(true);

    operation.dispose();
    expect(targets[0].exists).toBe(false);
    expect(targets[0].deleteCount).toBe(1);
  });

  test("cancels an in-flight download and deletes its partial file", async () => {
    const { backend, targets } = backendWithDownload(
      (target, options) =>
        new Promise((_resolve, reject) => {
          target.exists = true;
          options.signal.addEventListener("abort", () => {
            reject(new Error("aborted"));
          });
        }),
    );
    const operation = createSessionPdfStage(stageInput(), backend);
    operation.dispose();

    const error = await operation.result.catch((value) => value);
    expect(error).toBeInstanceOf(SessionPdfStageError);
    expect(error.cancelled).toBe(true);
    expect(targets[0].exists).toBe(false);
    expect(targets[0].deleteCount).toBeGreaterThanOrEqual(1);
  });

  test("isolates generations and disposes the previous generation", async () => {
    const { backend, targets } = backendWithDownload(async (target) => {
      target.exists = true;
      target.size = 128;
      return target;
    });
    const first = createSessionPdfStage(stageInput("generation-a", 1), backend);
    await first.result;
    const second = createSessionPdfStage(
      stageInput("generation-b", 2),
      backend,
    );
    await second.result;

    expect(targets[0].uri).not.toBe(targets[1].uri);
    first.dispose();
    expect(targets[0].exists).toBe(false);
    expect(targets[1].exists).toBe(true);
    second.dispose();
  });

  test("rejects and deletes size changes or oversized progress", async () => {
    const changed = backendWithDownload(async (target) => {
      target.exists = true;
      target.size = 129;
      return target;
    });
    const changedOperation = createSessionPdfStage(
      stageInput(),
      changed.backend,
    );
    const changedError = await changedOperation.result.catch((value) => value);
    expect(changedError).toBeInstanceOf(SessionPdfStageError);
    expect(changedError.stale).toBe(true);
    expect(changed.targets[0].exists).toBe(false);

    const oversized = backendWithDownload(async (target, options) => {
      target.exists = true;
      options.onProgress({ bytesWritten: 129, totalBytes: 129 });
      throw new Error("aborted");
    });
    const oversizedOperation = createSessionPdfStage(
      stageInput(),
      oversized.backend,
    );
    const oversizedError = await oversizedOperation.result.catch(
      (value) => value,
    );
    expect(oversizedError).toBeInstanceOf(SessionPdfStageError);
    expect(oversizedError.stale).toBe(true);
    expect(oversized.targets[0].exists).toBe(false);
  });

  test("projects a generation conflict as stale and sanitizes staging names", async () => {
    const { backend, targets } = backendWithDownload(async (target) => {
      target.exists = true;
      throw new Error("UnableToDownload: HTTP 409");
    });
    const operation = createSessionPdfStage(stageInput(), backend);
    const error = await operation.result.catch((value) => value);

    expect(error).toBeInstanceOf(SessionPdfStageError);
    expect(error.stale).toBe(true);
    expect(targets[0].exists).toBe(false);
    expect(sessionPdfStageFileName("../bad generation", "sheet:@1", 0)).toBe(
      "zen-session-pdf-badgeneration-sheet1-1.pdf",
    );
  });
});
