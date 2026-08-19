import { describe, expect, mock, test } from "bun:test";

const calls: Array<Record<string, unknown>> = [];
let downloadError: Error | undefined;
let copyError: Error | undefined;

class FakeFile {
  exists = true;

  readonly uri: string;

  constructor(...parts: string[]) {
    this.uri = parts.join("/").replace("file:////", "file:///");
  }

  copy(destination: FakeFile, options?: Record<string, unknown>) {
    calls.push({
      kind: "copy",
      source: this.uri,
      destination: destination.uri,
      options,
    });
    if (copyError) {
      throw copyError;
    }
    return Promise.resolve();
  }

  delete() {
    calls.push({ kind: "delete", uri: this.uri });
    this.exists = false;
  }

  writableStream() {
    throw new Error("writableStream must not be used by the Expo export path");
  }

  static async downloadFileAsync(
    uri: string,
    destination: FakeFile,
    options: Record<string, unknown>,
  ) {
    calls.push({ kind: "download", uri, destination: destination.uri, options });
    if (downloadError) {
      throw downloadError;
    }
    return destination;
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
  Paths: { cache: "file:///cache" },
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

  function resetFakeFailures() {
    downloadError = undefined;
    copyError = undefined;
  }

  test("uses a native cache download before copying into the owned SAF file", async () => {
    calls.length = 0;
    const backend = createExpoSessionFileDownloadBackend();
    const directory = await backend.pickDirectory();
    const destination = directory.reserve("notes.md", "text/plain");

    await backend.download("https://host.example/file?cap=1", destination, {
      headers: { "Cache-Control": "no-store" },
    });

    expect(calls[0]).toEqual({
      kind: "reserve",
      name: "notes.md",
      mimeType: "text/plain",
    });
    expect(calls[1]).toMatchObject({
      kind: "download",
      uri: "https://host.example/file?cap=1",
      options: {
        headers: { "Cache-Control": "no-store" },
        idempotent: true,
      },
    });
    expect(calls[1]?.options).not.toHaveProperty("expectedBytes");
    expect(calls[1]?.destination).toMatch(/^file:\/\/\/cache\/.zen-session-download-/);
    expect(calls[2]).toEqual({
      kind: "copy",
      source: calls[1]?.destination,
      destination: "content://picked/notes.md",
      options: { overwrite: true },
    });
    expect(calls[3]).toEqual({ kind: "delete", uri: calls[1]?.destination });
  });

  test("keeps expected byte validation in JavaScript instead of native options", async () => {
    calls.length = 0;
    resetFakeFailures();
    const { backend, destination } = await createDestination();

    await expect(
      backend.download("https://host.example/file", destination, {
        headers: {},
        expectedBytes: 7,
      }),
    ).rejects.toThrow("expected 7 bytes, received 0");

    expect(calls.find((call) => call.kind === "download")?.options).toEqual({
      headers: {},
      idempotent: true,
    });
  });

  test("cleans the temporary file after download failure, copy failure, or cancellation", async () => {
    for (const [failure, expectedMessage] of [
      ["download", "network failed"],
      ["copy", "destination failed"],
      ["download-cancel", "Download was cancelled"],
    ] as const) {
      calls.length = 0;
      resetFakeFailures();
      if (failure === "copy") {
        copyError = new Error(expectedMessage);
      } else {
        downloadError = new Error(expectedMessage);
      }

      const { backend, destination } = await createDestination();
      await expect(
        backend.download("https://host.example/file", destination, {
          headers: {},
        }),
      ).rejects.toThrow(expectedMessage);

      const temporaryUri = calls.find((call) => call.kind === "download")
        ?.destination;
      expect(calls).toContainEqual({ kind: "delete", uri: temporaryUri });
    }
    resetFakeFailures();
  });

  test("uses distinct hidden cache files for repeated downloads", async () => {
    calls.length = 0;
    resetFakeFailures();
    const { backend, destination } = await createDestination();

    await backend.download("https://host.example/one", destination, {
      headers: {},
    });
    await backend.download("https://host.example/two", destination, {
      headers: {},
    });

    const temporaryUris = calls
      .filter((call) => call.kind === "download")
      .map((call) => call.destination);
    expect(new Set(temporaryUris).size).toBe(2);
    expect(temporaryUris.every((uri) =>
      typeof uri === "string" && uri.startsWith("file:///cache/.zen-session-download-"),
    )).toBe(true);
    expect(calls.filter((call) => call.kind === "delete")).toHaveLength(2);
  });
});
