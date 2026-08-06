import { Directory } from "expo-file-system";
import { fetch } from "expo/fetch";
import type { SessionFileDownloadBackend } from "./sessionFilePreviewDownload";
import { createFetchSessionFileDownloadBackend } from "./sessionFilePreviewDownloadFetch";

/**
 * Production Expo wiring: Directory.createFile reserve + expo/fetch stream into
 * File.writableStream. Never uses File.downloadFileAsync.
 */
export function createExpoSessionFileDownloadBackend(): SessionFileDownloadBackend {
  return createFetchSessionFileDownloadBackend({
    async pickDirectory() {
      const directory = await Directory.pickDirectoryAsync();
      return {
        createFile(name, mimeType) {
          // Android SAF content:// requires Directory.createFile — File.create throws.
          const file = directory.createFile(name, mimeType);
          return {
            get uri() {
              return file.uri;
            },
            delete() {
              if (file.exists) {
                file.delete();
              }
            },
            writableStream() {
              return file.writableStream();
            },
          };
        },
      };
    },
    async fetch(url, init) {
      const response = await fetch(url, init);
      return {
        ok: response.ok,
        status: response.status,
        body: response.body,
      };
    },
  });
}
