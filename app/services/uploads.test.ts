import { afterAll, beforeEach, describe, expect, mock, test } from "bun:test";

const selectedAsset = {
  uri: "content://documents/archive.ZIP",
  name: "../../报告 2026.ZIP",
  mimeType: "application/zip",
  size: 1_532_564_736,
  lastModified: 0,
};
const manualServer = {
  id: "server-a",
  name: "Self-managed",
  url: "wss://zen.test/ws?stale=true",
  daemonId: "daemon-a",
  daemonPublicKey: "public-key-a",
  transportKind: "manual" as const,
};
let storedServer: Record<string, unknown> = manualServer;
let nativeTunnelStarts = 0;

let pickerOptions: unknown;
let authorizationOptions: unknown;
let selectedSize = selectedAsset.size;
let selectedName = selectedAsset.name;
let bytesCalls = 0;
let fetchCalls = 0;
let releaseCalls = 0;
let cancelCalls = 0;
let nativeUploadError: Error | null = null;
let nativeCancelError: Error | null = null;
let nativeUploadDeferred: ReturnType<
  typeof deferred<typeof uploadResult>
> | null = null;
let uploadResult = {
  status: 200,
  body: JSON.stringify({
    path: "/state/uploads/server-file.zip",
    name: selectedAsset.name,
  }),
  headers: {},
};
const uploadCalls: Array<{
  file: FakeFile;
  url: string;
  options: Record<string, unknown>;
}> = [];

class FakeFile {
  readonly name = selectedAsset.name;
  readonly type = selectedAsset.mimeType;

  constructor(readonly uri: string) {}

  bytes() {
    bytesCalls += 1;
    throw new Error("File.bytes must not be called by uploads");
  }

  createUploadTask(url: string, options: Record<string, unknown>) {
    uploadCalls.push({ file: this, url, options });
    return {
      uploadAsync: async () => {
        if (nativeUploadError) {
          throw nativeUploadError;
        }
        if (nativeUploadDeferred) {
          return nativeUploadDeferred.promise;
        }
        return uploadResult;
      },
      cancel: () => {
        cancelCalls += 1;
        if (nativeCancelError) {
          throw nativeCancelError;
        }
      },
      release: () => {
        releaseCalls += 1;
      },
    };
  }
}

mock.module("expo-document-picker", () => ({
  getDocumentAsync: async (options: unknown) => {
    pickerOptions = options;
    return {
      canceled: false,
      assets: [{ ...selectedAsset, name: selectedName, size: selectedSize }],
    };
  },
}));

mock.module("expo-file-system", () => ({
  Directory: {},
  File: FakeFile,
  UploadType: { BINARY_CONTENT: 0, MULTIPART: 1 },
}));

mock.module("./auth", () => ({
  buildAuthorizationHeader: async (options: unknown) => {
    authorizationOptions = options;
    return "Zen test-authorization";
  },
  getOrCreateLocalDeviceIdentity: async () => ({
    deviceId: "test-device",
    deviceName: "Test device",
    publicKeyHex: "6".repeat(64),
    seedHex: "7".repeat(64),
  }),
  normalizeDaemonId: (value: string | null | undefined) => value?.trim() || "",
  normalizePairingToken: (value: string | null | undefined) =>
    value?.trim() || "",
  normalizePublicKeyHex: (value: string | null | undefined) =>
    value?.trim() || "",
  verifyDaemonAssertion: () => true,
}));

mock.module("./storage", () => ({
  getServerById: async () => storedServer,
}));

mock.module("../modules/zen-link-transport/src", () => ({
  startPinnedTunnel: async () => {
    nativeTunnelStarts += 1;
    throw new Error("pinned candidate offline");
  },
  stopPinnedTunnel: async () => undefined,
}));

const originalFetch = globalThis.fetch;
Object.assign(globalThis, {
  fetch: async () => {
    fetchCalls += 1;
    throw new Error("fetch/FormData must not own file uploads");
  },
});

const {
  createAttachmentUploadOperation,
  uploadDocumentForServer,
  V1_MAX_UPLOAD_FILE_BYTES,
} = await import("./uploads");

afterAll(() => {
  Object.assign(globalThis, { fetch: originalFetch });
  mock.restore();
});

beforeEach(() => {
  pickerOptions = undefined;
  authorizationOptions = undefined;
  selectedSize = selectedAsset.size;
  selectedName = selectedAsset.name;
  bytesCalls = 0;
  fetchCalls = 0;
  releaseCalls = 0;
  cancelCalls = 0;
  nativeUploadError = null;
  nativeCancelError = null;
  nativeUploadDeferred = null;
  uploadCalls.length = 0;
  uploadResult = {
    status: 200,
    body: JSON.stringify({
      path: "/state/uploads/server-file.zip",
      name: selectedAsset.name,
    }),
    headers: {},
  };
  storedServer = manualServer;
  nativeTunnelStarts = 0;
});

describe("native attachment upload", () => {
  test("Link upload fails closed before creating a raw native upload task", async () => {
    storedServer = {
      ...manualServer,
      url: "wss://11111111111111111111111111111111.link.test/ws",
      transportKind: "link",
      transportPin: "3".repeat(64),
      linkRouteId: "4".repeat(32),
      transportCandidates: [
        {
          name: "region-a",
          kind: "link",
          url: "wss://11111111111111111111111111111111.link.test/ws",
        },
      ],
    };

    await expect(uploadDocumentForServer("server-a")).rejects.toThrow(
      "Zen Link is offline",
    );
    expect(nativeTunnelStarts).toBe(1);
    expect(uploadCalls).toHaveLength(0);
    expect(fetchCalls).toBe(0);
  });

  test("uses the selected file URI in an authenticated native binary task", async () => {
    const attachment = await uploadDocumentForServer("server-a");

    expect(pickerOptions).toEqual({
      type: ["*/*"],
      copyToCacheDirectory: false,
    });
    expect(uploadCalls).toHaveLength(1);
    expect(authorizationOptions).toEqual({
      daemonId: "daemon-a",
      purpose: "zen-upload",
    });
    expect(uploadCalls[0]).toEqual({
      file: expect.objectContaining({
        uri: selectedAsset.uri,
        name: selectedAsset.name,
      }),
      url: "https://zen.test/upload",
      options: {
        httpMethod: "POST",
        uploadType: 0,
        onProgress: expect.any(Function),
        headers: {
          Authorization: "Zen test-authorization",
          "Content-Type": "application/zip",
          "X-Zen-Upload-Name": "..%2F..%2F%E6%8A%A5%E5%91%8A%202026.ZIP",
        },
      },
    });
    expect(attachment).toEqual({
      name: selectedAsset.name,
      path: "/state/uploads/server-file.zip",
      localUri: selectedAsset.uri,
      mimeType: "application/zip",
    });
    expect(bytesCalls).toBe(0);
    expect(fetchCalls).toBe(0);
    expect(releaseCalls).toBe(1);
  });

  test("releases the native task when the source upload fails", async () => {
    nativeUploadError = new Error("selected source became unreadable");

    await expect(uploadDocumentForServer("server-a")).rejects.toThrow(
      "selected source became unreadable",
    );
    expect(bytesCalls).toBe(0);
    expect(fetchCalls).toBe(0);
    expect(releaseCalls).toBe(1);
  });

  test("surfaces a concise non-2xx native response without reading file bytes", async () => {
    uploadResult = {
      status: 507,
      body: "upload storage capacity reached\n",
      headers: {},
    };

    await expect(uploadDocumentForServer("server-a")).rejects.toThrow(
      "upload storage capacity reached",
    );
    expect(bytesCalls).toBe(0);
    expect(fetchCalls).toBe(0);
    expect(releaseCalls).toBe(1);
  });

  test("does not start a native transfer for a known file above the V1 limit", async () => {
    selectedSize = V1_MAX_UPLOAD_FILE_BYTES + 1;

    await expect(uploadDocumentForServer("server-a")).rejects.toThrow(
      "File exceeds the 2 GiB upload limit.",
    );
    expect(uploadCalls).toHaveLength(0);
    expect(bytesCalls).toBe(0);
    expect(fetchCalls).toBe(0);
    expect(releaseCalls).toBe(0);
  });

  test("does not start a native transfer for an overlong original name", async () => {
    selectedName = "a".repeat(1025);

    await expect(uploadDocumentForServer("server-a")).rejects.toThrow(
      "File name is too long.",
    );
    expect(uploadCalls).toHaveLength(0);
    expect(bytesCalls).toBe(0);
    expect(fetchCalls).toBe(0);
    expect(releaseCalls).toBe(0);
  });

  test("projects native progress monotonically for known and unknown totals", async () => {
    nativeUploadDeferred = deferred();
    const snapshots: unknown[] = [];
    const operation = createAttachmentUploadOperation(
      selectedAsset,
      manualServer,
      { onProgress: (progress) => snapshots.push(progress) },
    );
    await flushMicrotasks();
    const onProgress = uploadCalls[0].options.onProgress as (progress: {
      bytesSent: number;
      totalBytes: number;
    }) => void;

    onProgress({ bytesSent: 128, totalBytes: 0 });
    onProgress({ bytesSent: 640, totalBytes: 1024 });
    onProgress({ bytesSent: 512, totalBytes: 1024 });

    expect(snapshots).toEqual([
      { transferredBytes: 128, totalBytes: null, fraction: null },
      { transferredBytes: 640, totalBytes: 1024, fraction: 0.625 },
      { transferredBytes: 640, totalBytes: 1024, fraction: 0.625 },
    ]);
    nativeUploadDeferred.resolve(uploadResult);
    await expect(operation.result).resolves.toEqual(
      expect.objectContaining({ path: "/state/uploads/server-file.zip" }),
    );
  });

  test("reports average native throughput and ETA after a stable sample window", async () => {
    nativeUploadDeferred = deferred();
    const snapshots: unknown[] = [];
    let now = 1_000;
    const operation = createAttachmentUploadOperation(
      selectedAsset,
      manualServer,
      {
        now: () => now,
        onProgress: (progress) => snapshots.push(progress),
      },
    );
    await flushMicrotasks();
    const onProgress = uploadCalls[0].options.onProgress as (progress: {
      bytesSent: number;
      totalBytes: number;
    }) => void;

    now = 1_100;
    onProgress({ bytesSent: 128, totalBytes: 1024 });
    now = 1_500;
    onProgress({ bytesSent: 512, totalBytes: 1024 });

    expect(snapshots).toEqual([
      { transferredBytes: 128, totalBytes: 1024, fraction: 0.125 },
      {
        transferredBytes: 512,
        totalBytes: 1024,
        fraction: 0.5,
        bytesPerSecond: 1024,
        etaSeconds: 1,
      },
    ]);
    nativeUploadDeferred.resolve(uploadResult);
    await operation.result;
  });

  test("cancel calls the exact native task once and suppresses late progress/success", async () => {
    nativeUploadDeferred = deferred();
    const snapshots: unknown[] = [];
    const operation = createAttachmentUploadOperation(
      selectedAsset,
      manualServer,
      { onProgress: (progress) => snapshots.push(progress) },
    );
    await flushMicrotasks();
    const onProgress = uploadCalls[0].options.onProgress as (progress: {
      bytesSent: number;
      totalBytes: number;
    }) => void;

    onProgress({ bytesSent: 128, totalBytes: 1024 });
    expect(operation.cancel()).toBeNull();
    expect(operation.cancel()).toBeNull();
    onProgress({ bytesSent: 1024, totalBytes: 1024 });
    nativeUploadDeferred.resolve(uploadResult);

    await expect(operation.result).rejects.toThrow(
      "Attachment upload cancelled.",
    );
    expect(snapshots).toEqual([
      { transferredBytes: 128, totalBytes: 1024, fraction: 0.125 },
    ]);
    expect(cancelCalls).toBe(1);
    expect(releaseCalls).toBe(1);
  });

  test("reports a genuine native cancellation failure without retrying cancel", async () => {
    nativeUploadDeferred = deferred();
    nativeCancelError = new Error("native cancellation failed");
    const operation = createAttachmentUploadOperation(
      selectedAsset,
      manualServer,
    );
    await flushMicrotasks();

    expect(operation.cancel()?.message).toBe("native cancellation failed");
    expect(operation.cancel()).toBeNull();
    expect(cancelCalls).toBe(1);
    nativeUploadDeferred.resolve(uploadResult);
    await expect(operation.result).rejects.toThrow(
      "native cancellation failed",
    );
    expect(releaseCalls).toBe(1);
  });
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((nextResolve, nextReject) => {
    resolve = nextResolve;
    reject = nextReject;
  });
  return { promise, reject, resolve };
}

async function flushMicrotasks() {
  for (let index = 0; index < 10; index += 1) {
    await Promise.resolve();
  }
}
