import { Directory } from "expo-file-system";
import type { SessionFileDownloadBackend } from "./sessionFilePreviewDownload";
import { createFetchSessionFileDownloadBackend } from "./sessionFilePreviewDownloadFetch";

/**
 * Production Expo wiring for Android SAF.
 *
 * A picked directory returns `content://` files. Android's Expo relocation API
 * cannot reliably copy a private cache file into those handles, so the
 * authenticated response is streamed directly into the owned SAF file.
 */
export function createExpoSessionFileDownloadBackend(): SessionFileDownloadBackend {
  return createFetchSessionFileDownloadBackend({
    async pickDirectory() {
      const directory = await Directory.pickDirectoryAsync();
      return {
        createFile(name, mimeType) {
          const file = directory.createFile(name, mimeType);
          return {
            uri: file.uri,
            delete() {
              if (file.exists) file.delete();
            },
            writableStream() {
              return file.writableStream();
            },
          };
        },
      };
    },
    async fetch(url, init) {
      const response = await fetch(url, {
        method: init.method,
        headers: init.headers,
      });
      return {
        ok: response.ok,
        status: response.status,
        body: response.body,
      };
    },
  });
}
