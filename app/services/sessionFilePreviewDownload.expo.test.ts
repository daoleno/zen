import { describe, expect, mock, test } from "bun:test";

const calls: Array<Record<string, unknown>> = [];
let fetchImpl: (url: string, init: RequestInit) => Promise<Response>;
let platformOS = "android";
let nativeAvailable = true;

class FakeFile {
  exists = true;
  readonly uri: string;
  readonly chunks: Uint8Array[] = [];

  constructor(...parts: string[]) {
    this.uri = parts.join("/").replace("file:////", "file:///");
  }

  delete() {
    calls.push({ kind: "delete", uri: this.uri });
    this.exists = false;
  }

  writableStream() {
    calls.push({ kind: "writable", uri: this.uri });
    return new WritableStream<Uint8Array>({
      write: (chunk) => {
        this.chunks.push(chunk);
      },
    });
  }
}

const pickedFile = new FakeFile("content://picked/notes.md");
const pickedDirectory = {
  createFile(name: string, mimeType: string) {
    calls.push({ kind: "reserve", name, mimeType });
    return pickedFile;
  },
};

mock.module("expo-file-system", () => ({
  Directory: {
    pickDirectoryAsync: async () => pickedDirectory,
  },
  File: FakeFile,
  UploadType: { BINARY_CONTENT: 0, MULTIPART: 1 },
}));

mock.module("react-native", () => ({
  Platform: {
    get OS() {
      return platformOS;
    },
  },
}));

mock.module("../modules/zen-file-upload/src", () => ({
  getZenFileDownloadModule() {
    if (!nativeAvailable) return null;
    return {
      async download(request: Record<string, unknown>) {
        calls.push({ kind: "native-download", request });
        return { bytesWritten: request.expectedSize ?? 0 };
      },
    };
  },
}));

const { createExpoSessionFileDownloadBackend } = await import(
  "./sessionFilePreviewDownload.expo"
);

describe("Expo Session file download backend", () => {
  function createDestination() {
    const backend = createExpoSessionFileDownloadBackend();
    return backend.pickDirectory().then((directory) => ({
      backend,
      destination: directory.reserve("notes.md", "text/plain"),
    }));
  }

  function setFetch(body: string, status = 200) {
    fetchImpl = async (url, init) => {
      calls.push({ kind: "fetch", url, init });
      return new Response(body, { status });
    };
    globalThis.fetch = fetchImpl as typeof globalThis.fetch;
  }

  test("uses the native Android transport instead of a JS response stream", async () => {
    calls.length = 0;
    platformOS = "android";
    nativeAvailable = true;
    const { backend, destination } = await createDestination();

    await backend.download("https://host.example/file?cap=1", destination, {
      headers: { "Cache-Control": "no-store" },
      expectedBytes: 7,
    });

    expect(calls[0]).toEqual({
      kind: "reserve",
      name: "notes.md",
      mimeType: "text/plain",
    });
    expect(calls[1]).toMatchObject({
      kind: "native-download",
      request: {
        url: "https://host.example/file?cap=1",
        destinationUri: "content://picked/notes.md",
        expectedSize: 7,
        maxBytes: 50 * 1024 * 1024,
        headers: { "Cache-Control": "no-store" },
      },
    });
    expect(calls.some((call) => call.kind === "fetch")).toBe(false);
    expect(calls.some((call) => call.kind === "writable")).toBe(false);
  });

  test("an old Android build fails safely before opening the directory picker", async () => {
    calls.length = 0;
    platformOS = "android";
    nativeAvailable = false;
    const backend = createExpoSessionFileDownloadBackend();

    await expect(backend.pickDirectory()).rejects.toThrow(
      "does not include the native Android download transport",
    );
    expect(calls).toEqual([]);
  });

  test("keeps the response-stream backend on non-Android platforms", async () => {
    calls.length = 0;
    platformOS = "ios";
    nativeAvailable = false;
    setFetch("payload");
    const { backend, destination } = await createDestination();

    await backend.download("https://host.example/file", destination, {
      headers: {},
      expectedBytes: 7,
    });
    expect(calls.some((call) => call.kind === "fetch")).toBe(true);
    expect(calls.some((call) => call.kind === "writable")).toBe(true);
  });
});
