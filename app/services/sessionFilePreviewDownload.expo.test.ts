import { describe, expect, mock, test } from "bun:test";

const calls: Array<Record<string, unknown>> = [];
let fetchImpl: (url: string, init: RequestInit) => Promise<Response>;

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

  test("streams the authenticated response directly into the owned SAF file", async () => {
    calls.length = 0;
    setFetch("payload");
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
      kind: "fetch",
      url: "https://host.example/file?cap=1",
      init: {
        method: "GET",
        headers: { "Cache-Control": "no-store" },
      },
    });
    expect(calls[2]).toEqual({
      kind: "writable",
      uri: "content://picked/notes.md",
    });
    expect(new TextDecoder().decode(pickedFile.chunks[0])).toBe("payload");
    expect(calls.some((call) => call.kind === "copy")).toBe(false);
  });

  test("validates the response size in JavaScript", async () => {
    calls.length = 0;
    setFetch("short");
    const { backend, destination } = await createDestination();

    await expect(
      backend.download("https://host.example/file", destination, {
        headers: {},
        expectedBytes: 7,
      }),
    ).rejects.toThrow("expected 7 bytes, received 5");
  });

  test("surfaces HTTP failures before opening the owned SAF file", async () => {
    calls.length = 0;
    setFetch("", 500);
    const { backend, destination } = await createDestination();

    await expect(
      backend.download("https://host.example/file", destination, {
        headers: {},
      }),
    ).rejects.toThrow("HTTP 500");
    expect(calls.some((call) => call.kind === "writable")).toBe(false);
  });
});
