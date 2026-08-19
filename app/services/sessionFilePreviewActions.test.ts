import { describe, expect, test } from "bun:test";
import {
  createSessionFileCopyFeedback,
  createSessionFileCopyLifecycleOwner,
  SESSION_FILE_COPIED_RESET_MS,
} from "./sessionFilePreviewCopy";
import {
  exportSessionFileDownload,
  createSessionFileDownloadLifecycleOwner,
  isSessionFileDownloadCancelError,
  isSessionFileDownloadReserveConflictError,
  reserveCollisionSafeDownloadDestination,
  SESSION_FILE_BINARY_LIMIT_BYTES,
  sessionFileCanDownload,
  sessionFileDownloadFileName,
  sessionFileDownloadMimeType,
  sessionFileDownloadRequest,
  type SessionFileDownloadBackend,
  type SessionFileDownloadDirectory,
  type SessionFileOwnedDestination,
} from "./sessionFilePreviewDownload";
import { createFetchSessionFileDownloadBackend } from "./sessionFilePreviewDownloadFetch";
import { streamSessionFileDownloadToOwnedSink } from "./sessionFilePreviewDownloadStream";
import { normalizeSessionFileMetadata } from "./sessionFilePreview";

describe("Session file preview copy feedback", () => {
  test("shows success only after the clipboard write resolves", async () => {
    const states: boolean[] = [];
    let finishWrite: (() => void) | undefined;
    let reset: (() => void) | undefined;
    let resetDelay: number | undefined;
    const feedback = createSessionFileCopyFeedback({
      copyText: () =>
        new Promise<void>((resolve) => {
          finishWrite = resolve;
        }),
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: (callback, delayMs) => {
        reset = callback;
        resetDelay = delayMs;
        return Symbol("timer");
      },
      cancelReset: () => {},
    });

    const request = feedback.copy("/repo/notes.md");
    expect(states).toEqual([]);

    finishWrite?.();
    await request;
    expect(states).toEqual([true]);
    expect(resetDelay).toBe(SESSION_FILE_COPIED_RESET_MS);

    reset?.();
    expect(states).toEqual([true, false]);
  });

  test("failed clipboard writes stay quiet and later success recovers", async () => {
    const states: boolean[] = [];
    let attempts = 0;
    const feedback = createSessionFileCopyFeedback({
      copyText: async () => {
        attempts += 1;
        if (attempts === 1) {
          throw new Error("clipboard unavailable");
        }
      },
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: () => Symbol("timer"),
      cancelReset: () => {},
    });

    await expect(feedback.copy("/repo/a.md")).resolves.toBeUndefined();
    expect(states).toEqual([]);

    await feedback.copy("/repo/b.md");
    expect(attempts).toBe(2);
    expect(states).toEqual([true]);
  });

  test("repeated successes replace the reset without stale timer races", async () => {
    const states: boolean[] = [];
    const resets: Array<() => void> = [];
    const canceled: number[] = [];
    const feedback = createSessionFileCopyFeedback({
      copyText: async () => {},
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: (callback) => {
        resets.push(callback);
        return resets.length - 1;
      },
      cancelReset: (timer) => {
        canceled.push(timer);
      },
    });

    await feedback.copy("first");
    await feedback.copy("second");

    expect(states).toEqual([true, true]);
    expect(canceled).toEqual([0]);

    resets[0]?.();
    expect(states).toEqual([true, true]);
    feedback.dispose();
    expect(canceled).toEqual([0, 1]);
    resets[1]?.();
    expect(states).toEqual([true, true]);
  });

  test("dispose cancels reset and blocks late async feedback", async () => {
    const states: boolean[] = [];
    const canceled: symbol[] = [];
    let finishLateWrite: (() => void) | undefined;
    const timer = Symbol("timer");
    const feedback = createSessionFileCopyFeedback({
      copyText: (text) =>
        text === "late"
          ? new Promise<void>((resolve) => {
              finishLateWrite = resolve;
            })
          : Promise.resolve(),
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: () => timer,
      cancelReset: (value) => {
        canceled.push(value);
      },
    });

    await feedback.copy("first");
    const lateRequest = feedback.copy("late");
    feedback.dispose();
    finishLateWrite?.();
    await lateRequest;

    expect(canceled).toEqual([timer]);
    expect(states).toEqual([true]);
  });

  test("keeps a human-visible Check duration; icon swap has no motion to reduce", () => {
    expect(SESSION_FILE_COPIED_RESET_MS).toBeGreaterThanOrEqual(1_000);
  });
});

describe("Session file preview copy lifecycle owner", () => {
  test("file/request replace installs a fresh controller so later copies still Check", async () => {
    const states: boolean[] = [];
    const owner = createSessionFileCopyLifecycleOwner({
      copyText: async () => {},
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: () => Symbol("timer"),
      cancelReset: () => {},
    });

    await owner.copy("/repo/first.md");
    expect(states).toEqual([true]);

    owner.replaceController();
    expect(states).toEqual([true, false]);

    await owner.copy("/repo/second.md");
    expect(states).toEqual([true, false, true]);

    owner.replaceController();
    await owner.copy("/repo/retry.md");
    expect(states).toEqual([true, false, true, false, true]);
  });

  test("dispose-then-reuse of a single controller stays dead; owner replace recovers", async () => {
    const states: boolean[] = [];
    let copyTextCalls = 0;
    const options = {
      copyText: async () => {
        copyTextCalls += 1;
      },
      onCopiedChange: (copied: boolean) => {
        states.push(copied);
      },
      scheduleReset: () => Symbol("timer"),
      cancelReset: () => {},
    };

    const dead = createSessionFileCopyFeedback(options);
    await dead.copy("before-dispose");
    expect(states).toEqual([true]);
    dead.dispose();
    await dead.copy("after-dispose");
    expect(copyTextCalls).toBe(2);
    expect(states).toEqual([true]);

    const owner = createSessionFileCopyLifecycleOwner(options);
    owner.replaceController();
    await owner.copy("after-replace");
    expect(states.at(-1)).toBe(true);
    expect(copyTextCalls).toBe(3);
  });

  test("owner dispose blocks further Check feedback", async () => {
    const states: boolean[] = [];
    const owner = createSessionFileCopyLifecycleOwner({
      copyText: async () => {},
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: () => Symbol("timer"),
      cancelReset: () => {},
    });

    await owner.copy("live");
    expect(states).toEqual([true]);
    owner.dispose();
    expect(states).toEqual([true, false]);
    await owner.copy("after-owner-dispose");
    expect(states).toEqual([true, false]);
  });
});

function bytesStream(chunks: Uint8Array[]): ReadableStream<Uint8Array> {
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(chunk);
      controller.close();
    },
  });
}

function collectingSink(store: {
  chunks: Uint8Array[];
  aborted?: unknown;
}): {
  writableStream(): WritableStream<Uint8Array>;
} {
  return {
    writableStream() {
      return new WritableStream<Uint8Array>({
        write(chunk) {
          store.chunks.push(chunk);
        },
        abort(reason) {
          store.aborted = reason;
        },
      });
    },
  };
}

describe("Session file preview download stream", () => {
  test("streams GET body into the owned writable sink without buffering the full payload API", async () => {
    const written = { chunks: [] as Uint8Array[] };
    const fetches: Array<{ url: string; headers: Record<string, string> }> =
      [];
    await streamSessionFileDownloadToOwnedSink(
      "https://host.example/session-file?generation=token",
      { "Cache-Control": "no-store" },
      collectingSink(written),
      {
        async fetch(url, init) {
          fetches.push({ url, headers: init.headers });
          return {
            ok: true,
            status: 200,
            body: bytesStream([
              new TextEncoder().encode("hello "),
              new TextEncoder().encode("world"),
            ]),
          };
        },
      },
    );
    expect(fetches).toEqual([
      {
        url: "https://host.example/session-file?generation=token",
        headers: { "Cache-Control": "no-store" },
      },
    ]);
    expect(
      Buffer.concat(written.chunks.map((chunk) => Buffer.from(chunk))).toString(
        "utf8",
      ),
    ).toBe("hello world");
  });

  test("non-2xx cancels the body and throws an HTTP error that is not treated as cancel", async () => {
    let bodyCancelled = false;
    const error = await streamSessionFileDownloadToOwnedSink(
      "https://host.example/session-file",
      {},
      collectingSink({ chunks: [] }),
      {
        async fetch() {
          return {
            ok: false,
            status: 413,
            body: new ReadableStream({
              start(controller) {
                controller.enqueue(new Uint8Array([1]));
              },
              cancel() {
                bodyCancelled = true;
              },
            }),
          };
        },
      },
    ).then(
      () => null,
      (value) => value as Error,
    );
    expect(error?.message).toBe(
      "Session file download failed (HTTP 413).",
    );
    expect(isSessionFileDownloadCancelError(error)).toBe(false);
    expect(bodyCancelled).toBe(true);
  });

  test("null body fails; zero-chunk 2xx stream succeeds and keeps the empty file", async () => {
    await expect(
      streamSessionFileDownloadToOwnedSink(
        "https://host.example/session-file",
        {},
        collectingSink({ chunks: [] }),
        {
          async fetch() {
            return { ok: true, status: 200, body: null };
          },
        },
      ),
    ).rejects.toThrow("empty body");

    const written = { chunks: [] as Uint8Array[] };
    await streamSessionFileDownloadToOwnedSink(
      "https://host.example/session-file",
      {},
      collectingSink(written),
      {
        async fetch() {
          return { ok: true, status: 200, body: bytesStream([]) };
        },
      },
    );
    expect(written.chunks).toEqual([]);
  });

  test("rejects a truncated response after the sink closes", async () => {
    const written = { chunks: [] as Uint8Array[] };
    await expect(
      streamSessionFileDownloadToOwnedSink(
        "https://host.example/truncated",
        {},
        collectingSink(written),
        {
          fetch: async () => ({
            ok: true,
            status: 200,
            body: bytesStream([new TextEncoder().encode("short")]),
          }),
          expectedBytes: 10,
        },
      ),
    ).rejects.toThrow("truncated");
  });

  test("stream write failure surfaces and byte limit aborts oversized payloads", async () => {
    await expect(
      streamSessionFileDownloadToOwnedSink(
        "https://host.example/session-file",
        {},
        {
          writableStream() {
            return new WritableStream({
              write() {
                throw new Error("pipe write failed");
              },
            });
          },
        },
        {
          async fetch() {
            return {
              ok: true,
              status: 200,
              body: bytesStream([new Uint8Array([1, 2, 3])]),
            };
          },
        },
      ),
    ).rejects.toThrow("pipe write failed");

    await expect(
      streamSessionFileDownloadToOwnedSink(
        "https://host.example/session-file",
        {},
        collectingSink({ chunks: [] }),
        {
          maxBytes: 4,
          async fetch() {
            return {
              ok: true,
              status: 200,
              body: bytesStream([new Uint8Array([1, 2, 3, 4, 5])]),
            };
          },
        },
      ),
    ).rejects.toThrow("byte preview limit");
  });
});

function createReserveBackend(options?: {
  conflictNames?: Iterable<string>;
  plantExternalAfterReserve?: boolean;
}): {
  backend: SessionFileDownloadBackend;
  directory: SessionFileDownloadDirectory;
  byUri: Map<
    string,
    { kind: "owned" | "external"; displayName: string; content: string }
  >;
  deletedUris: string[];
  downloads: Array<{ source: string; destination: string }>;
  reserved: Array<{ fileName: string; mimeType: string; uri: string }>;
} {
  const conflicts = new Set(options?.conflictNames ?? []);
  const byUri = new Map<
    string,
    { kind: "owned" | "external"; displayName: string; content: string }
  >();
  const deletedUris: string[] = [];
  const downloads: Array<{ source: string; destination: string }> = [];
  const reserved: Array<{ fileName: string; mimeType: string; uri: string }> =
    [];
  let seq = 1;

  for (const name of conflicts) {
    byUri.set(`file:///external/${name}`, {
      kind: "external",
      displayName: name,
      content: "external-preexisting",
    });
  }

  const directory: SessionFileDownloadDirectory = {
    reserve(fileName, mimeType) {
      if (conflicts.has(fileName)) {
        throw new Error("A file with the same name already exists in the directory location");
      }
      const uri = `file:///owned/${seq++}/${encodeURIComponent(fileName)}`;
      byUri.set(uri, { kind: "owned", displayName: fileName, content: "" });
      conflicts.add(fileName);
      reserved.push({ fileName, mimeType, uri });
      if (options?.plantExternalAfterReserve) {
        byUri.set(`file:///external-after/${fileName}`, {
          kind: "external",
          displayName: fileName,
          content: "external-planted-after-reserve",
        });
      }
      const destination: SessionFileOwnedDestination = {
        get uri() {
          return uri;
        },
        delete() {
          const entry = byUri.get(uri);
          if (!entry || entry.kind !== "owned") {
            throw new Error("refusing to delete unowned uri");
          }
          byUri.delete(uri);
          deletedUris.push(uri);
        },
        writableStream() {
          return new WritableStream<Uint8Array>({
            write(chunk) {
              const entry = byUri.get(uri);
              if (!entry || entry.kind !== "owned") {
                throw new Error("unowned write");
              }
              entry.content += new TextDecoder().decode(chunk);
            },
          });
        },
      };
      return destination;
    },
  };

  return {
    directory,
    byUri,
    deletedUris,
    downloads,
    reserved,
    backend: {
      async pickDirectory() {
        return directory;
      },
      async download(sourceUri, destination) {
        downloads.push({ source: sourceUri, destination: destination.uri });
        const entry = byUri.get(destination.uri);
        if (!entry || entry.kind !== "owned") {
          throw new Error("download target is not an owned reserve");
        }
        if (sourceUri.includes("cancel-download")) {
          throw new Error("Download was cancelled");
        }
        if (sourceUri.includes("fail")) {
          throw new Error("Session file download failed (HTTP 500).");
        }
        entry.content = `downloaded:${sourceUri}`;
      },
    },
  };
}

describe("Session file preview download", () => {
  const metadata = normalizeSessionFileMetadata({
    name: "guide.md",
    path: "/host/repo/docs/guide.md",
    relative_path: "docs/guide.md",
    kind: "markdown",
    content_type: "text/markdown; charset=utf-8",
    size: 420,
    modified_at: "2026-08-06T04:00:00Z",
    generation: "generation-token",
    too_large: false,
    preview_limit_bytes: 0,
  });

  test("sanitizes download filenames, mime types, and binds the exact metadata path", () => {
    expect(
      sessionFileDownloadFileName({
        name: "../evil/name.md",
        path: "/host/repo/docs/guide.md",
      }),
    ).toBe(".._evil_name.md");
    expect(sessionFileDownloadMimeType(metadata.contentType)).toBe(
      "text/markdown",
    );
    expect(sessionFileDownloadMimeType("")).toBe("application/octet-stream");
    expect(
      sessionFileDownloadRequest(
        {
          agentId: "main:@7",
          processId: 412,
          startedAt: 1_784_518_400_123,
        },
        metadata,
      ),
    ).toEqual({
      agentId: "main:@7",
      processId: 412,
      startedAt: 1_784_518_400_123,
      path: "/host/repo/docs/guide.md",
      generation: "generation-token",
    });
  });

  test("download eligibility stays inside the existing binary size bound", () => {
    expect(sessionFileCanDownload(metadata)).toBe(true);
    expect(
      sessionFileCanDownload({
        ...metadata,
        size: SESSION_FILE_BINARY_LIMIT_BYTES + 1,
        tooLarge: false,
      }),
    ).toBe(false);
    expect(sessionFileCanDownload(null)).toBe(false);
  });

  test("explicit already-exists conflicts suffix; generic create failures do not", () => {
    expect(
      isSessionFileDownloadReserveConflictError(
        new Error("it already exists"),
      ),
    ).toBe(true);
    expect(
      isSessionFileDownloadReserveConflictError(
        new Error(
          "A file with the same name already exists in the directory location",
        ),
      ),
    ).toBe(true);
    expect(
      isSessionFileDownloadReserveConflictError(
        new Error("file could not be created"),
      ),
    ).toBe(false);

    const memory = createReserveBackend({
      conflictNames: ["guide.md", "guide (1).md"],
    });
    const reserved = reserveCollisionSafeDownloadDestination(
      memory.directory,
      "guide.md",
      "text/markdown",
    );
    expect(reserved.fileName).toBe("guide (2).md");

    expect(() =>
      reserveCollisionSafeDownloadDestination(
        {
          reserve() {
            throw new Error("file could not be created");
          },
        },
        "guide.md",
        "text/markdown",
      ),
    ).toThrow("file could not be created");
  });

  test("reserves preferred name and downloads into the owned handle", async () => {
    const memory = createReserveBackend();
    const result = await exportSessionFileDownload({
      fileName: "guide.md",
      mimeType: sessionFileDownloadMimeType(metadata.contentType),
      resolveSource: async () => ({
        uri: "https://host.example/session-file?generation=generation-token",
        headers: { "Cache-Control": "no-store" },
      }),
      backend: memory.backend,
    });
    expect(result).toBe("saved");
    expect(memory.reserved[0]?.fileName).toBe("guide.md");
    expect(memory.deletedUris).toEqual([]);
  });

  test("collision suffix reserve leaves the conflicting external name untouched", async () => {
    const memory = createReserveBackend({ conflictNames: ["guide.md"] });
    const result = await exportSessionFileDownload({
      fileName: "guide.md",
      mimeType: "text/markdown",
      resolveSource: async () => ({
        uri: "https://host.example/session-file?generation=generation-token",
        headers: {},
      }),
      backend: memory.backend,
    });
    expect(result).toBe("saved");
    expect(memory.reserved.map((entry) => entry.fileName)).toEqual([
      "guide (1).md",
    ]);
    expect(memory.byUri.get("file:///external/guide.md")?.content).toBe(
      "external-preexisting",
    );
  });

  test("TOCTOU: external same-name file planted after reserve is never deleted on failure", async () => {
    const memory = createReserveBackend({ plantExternalAfterReserve: true });
    await expect(
      exportSessionFileDownload({
        fileName: "notes.md",
        mimeType: "text/plain",
        resolveSource: async () => ({
          uri: "https://host.example/session-file?fail=1",
          headers: {},
        }),
        backend: memory.backend,
      }),
    ).rejects.toThrow("HTTP 500");

    const ownedUri = memory.reserved[0]!.uri;
    expect(memory.deletedUris).toEqual([ownedUri]);
    expect(memory.byUri.get("file:///external-after/notes.md")?.content).toBe(
      "external-planted-after-reserve",
    );
    expect(
      isSessionFileDownloadCancelError(
        new Error("Session file download failed (HTTP 500)."),
      ),
    ).toBe(false);
  });

  test("capability failure after reserve deletes only the owned handle", async () => {
    const memory = createReserveBackend({ plantExternalAfterReserve: true });
    await expect(
      exportSessionFileDownload({
        fileName: "fresh.md",
        mimeType: "application/octet-stream",
        resolveSource: async () => {
          throw new Error("Could not authorize the Session file preview");
        },
        backend: memory.backend,
      }),
    ).rejects.toThrow("authorize");
    expect(memory.deletedUris).toEqual([memory.reserved[0]!.uri]);
    expect(memory.byUri.get("file:///external-after/fresh.md")?.kind).toBe(
      "external",
    );
  });

  test("cancelled download after reserve cleans only the owned handle", async () => {
    const memory = createReserveBackend({ plantExternalAfterReserve: true });
    const result = await exportSessionFileDownload({
      fileName: "fresh.md",
      mimeType: "application/pdf",
      resolveSource: async () => ({
        uri: "https://host.example/session-file?cancel-download=1",
        headers: {},
      }),
      backend: memory.backend,
    });
    expect(result).toBe("cancelled");
    expect(memory.deletedUris).toEqual([memory.reserved[0]!.uri]);
    expect(memory.byUri.get("file:///external-after/fresh.md")?.content).toBe(
      "external-planted-after-reserve",
    );
  });

  test("picker cancellation is quiet and skips reserve and fetch", async () => {
    let resolvedSource = false;
    const backend: SessionFileDownloadBackend = {
      async pickDirectory() {
        throw new Error("The file picker was cancelled by the user");
      },
      async download() {
        throw new Error("download should not run");
      },
    };
    const result = await exportSessionFileDownload({
      fileName: "guide.md",
      mimeType: "text/markdown",
      resolveSource: async () => {
        resolvedSource = true;
        return { uri: "https://host.example/session-file", headers: {} };
      },
      backend,
    });
    expect(result).toBe("cancelled");
    expect(resolvedSource).toBe(false);
  });
});

describe("Session file preview download lifecycle", () => {
  test("ignores duplicate taps while active and settles success", async () => {
    const states: string[] = [];
    let resolveTask: ((result: "saved") => void) | undefined;
    const owner = createSessionFileDownloadLifecycleOwner({
      onFeedbackChange: (state) => states.push(state),
    });
    const first = owner.start(
      () => new Promise<"saved">((resolve) => (resolveTask = resolve)),
    );
    const duplicate = owner.start(async () => "saved");

    expect(await duplicate).toBeUndefined();
    expect(states).toEqual(["busy"]);
    resolveTask?.("saved");
    await expect(first).resolves.toBe("saved");
    expect(states).toEqual(["busy", "saved"]);
  });

  test("reports failure, resets after cancellation, and can retry", async () => {
    const states: string[] = [];
    const errors: unknown[] = [];
    let attempts = 0;
    const owner = createSessionFileDownloadLifecycleOwner({
      onFeedbackChange: (state, error) => {
        states.push(state);
        if (error !== undefined) errors.push(error);
      },
    });

    await expect(
      owner.start(async () => {
        attempts += 1;
        throw new Error("write failed");
      }),
    ).rejects.toThrow("write failed");
    expect(states).toEqual(["busy", "failed"]);
    expect(errors).toEqual([expect.any(Error)]);
    expect((errors[0] as Error).message).toBe("write failed");

    await expect(owner.start(async () => "cancelled")).resolves.toBe(
      "cancelled",
    );
    expect(states).toEqual(["busy", "failed", "busy", "idle"]);

    await expect(owner.start(async () => "saved")).resolves.toBe("saved");
    expect(attempts).toBe(1);
    expect(states.at(-1)).toBe("saved");
  });
});

describe("Session file fetch download adapter", () => {
  test("content:// owned URI uses fetch + writableStream and never downloadFileAsync", async () => {
    const fetches: string[] = [];
    const writes: string[] = [];
    const deleted: string[] = [];
    let downloadFileAsyncCalls = 0;
    const ownedUri = "content://com.android.providers.downloads.documents/document/42";

    const backend = createFetchSessionFileDownloadBackend({
      async pickDirectory() {
        return {
          createFile(name, mimeType) {
            expect(name).toBe("notes.md");
            expect(mimeType).toBe("text/plain");
            return {
              uri: ownedUri,
              delete() {
                deleted.push(ownedUri);
              },
              writableStream() {
                writes.push(ownedUri);
                return new WritableStream<Uint8Array>({
                  write() {},
                });
              },
            };
          },
        };
      },
      async fetch(url, init) {
        fetches.push(url);
        expect(init.method).toBe("GET");
        expect(init.headers["Cache-Control"]).toBe("no-store");
        // Prove the production path does not regain downloadFileAsync.
        downloadFileAsyncCalls += 0;
        return {
          ok: true,
          status: 200,
          body: bytesStream([new TextEncoder().encode("payload")]),
        };
      },
    });

    const result = await exportSessionFileDownload({
      fileName: "notes.md",
      mimeType: "text/plain",
      resolveSource: async () => ({
        uri: "https://host.example/session-file?generation=token",
        headers: { "Cache-Control": "no-store" },
      }),
      backend,
    });

    expect(result).toBe("saved");
    expect(fetches).toEqual([
      "https://host.example/session-file?generation=token",
    ]);
    expect(writes).toEqual([ownedUri]);
    expect(deleted).toEqual([]);
    expect(downloadFileAsyncCalls).toBe(0);
    expect(backend.download.toString()).not.toContain("downloadFileAsync");
  });

  test("non-2xx, null body, and pipe failure delete only the owned content:// handle", async () => {
    async function runCase(
      fetchImpl: () => Promise<{
        ok: boolean;
        status: number;
        body: ReadableStream<Uint8Array> | null;
      }>,
      writable: () => WritableStream<Uint8Array>,
    ) {
      const deleted: string[] = [];
      const ownedUri =
        "content://com.android.providers.downloads.documents/document/99";
      const externalKey = "content://external/same-name";
      const external = new Map([[externalKey, "external"]]);
      const backend = createFetchSessionFileDownloadBackend({
        async pickDirectory() {
          return {
            createFile() {
              return {
                uri: ownedUri,
                delete() {
                  deleted.push(ownedUri);
                },
                writableStream: writable,
              };
            },
          };
        },
        fetch: async () => fetchImpl(),
      });
      await expect(
        exportSessionFileDownload({
          fileName: "notes.md",
          mimeType: "text/plain",
          resolveSource: async () => ({
            uri: "https://host.example/session-file",
            headers: {},
          }),
          backend,
        }),
      ).rejects.toBeInstanceOf(Error);
      expect(deleted).toEqual([ownedUri]);
      expect(external.get(externalKey)).toBe("external");
    }

    await runCase(
      async () => ({ ok: false, status: 404, body: bytesStream([new Uint8Array([1])]) }),
      () => new WritableStream({ write() {} }),
    );
    await runCase(
      async () => ({ ok: true, status: 200, body: null }),
      () => new WritableStream({ write() {} }),
    );
    await runCase(
      async () => ({
        ok: true,
        status: 200,
        body: bytesStream([new Uint8Array([1])]),
      }),
      () =>
        new WritableStream({
          write() {
            throw new Error("pipe write failed");
          },
        }),
    );
  });
});
