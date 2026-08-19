import { describe, expect, mock, test } from "bun:test";

const calls: Array<Record<string, unknown>> = [];

class FakeFile {
  exists = true;

  readonly uri: string;

  constructor(...parts: string[]) {
    this.uri = parts.join("/").replace("file:////", "file:///");
  }

  copy(destination: FakeFile) {
    calls.push({ kind: "copy", source: this.uri, destination: destination.uri });
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

describe("Expo Session file download backend", () => {
  test("uses a native cache download before copying into the owned SAF file", async () => {
    const { createExpoSessionFileDownloadBackend } = await import(
      "./sessionFilePreviewDownload.expo"
    );
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
      options: { headers: { "Cache-Control": "no-store" } },
    });
    expect(calls[1]?.destination).toMatch(/^file:\/\/\/cache\/.zen-session-download-/);
    expect(calls[2]).toEqual({
      kind: "copy",
      source: calls[1]?.destination,
      destination: "content://picked/notes.md",
    });
    expect(calls[3]).toEqual({ kind: "delete", uri: calls[1]?.destination });
  });
});
