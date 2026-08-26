import { Directory } from "expo-file-system";
import { Platform } from "react-native";
import { getZenFileDownloadModule } from "../modules/zen-file-upload/src";
import type { SessionFileDownloadBackend } from "./sessionFilePreviewDownload";
import { SESSION_FILE_BINARY_LIMIT_BYTES } from "./sessionFilePreviewDownload";
import { createFetchSessionFileDownloadBackend } from "./sessionFilePreviewDownloadFetch";

let downloadSequence = 0;

function nextDownloadId(): string {
  downloadSequence += 1;
  return `session-file-${Date.now().toString(36)}-${downloadSequence.toString(36)}`;
}

function createNativeAndroidDownloadBackend(): SessionFileDownloadBackend {
  const native = getZenFileDownloadModule();
  return {
    async pickDirectory() {
      if (!native?.download) {
        throw new Error(
          "This Zen build does not include the native Android download transport. Install the latest Android build and try again.",
        );
      }
      const directory = await Directory.pickDirectoryAsync();
      return {
        reserve(name, mimeType) {
          const file = directory.createFile(name, mimeType);
          return {
            uri: file.uri,
            delete() {
              if (file.exists) file.delete();
            },
            writableStream() {
              throw new Error("Android downloads must use the native transport.");
            },
          };
        },
      };
    },
    async download(uri, destination, options) {
      if (!native?.download) {
        throw new Error(
          "This Zen build does not include the native Android download transport.",
        );
      }
      await native.download({
        downloadId: nextDownloadId(),
        url: uri,
        destinationUri: destination.uri,
        expectedSize: options.expectedBytes ?? null,
        maxBytes: SESSION_FILE_BINARY_LIMIT_BYTES,
        headers: options.headers,
      });
    },
  };
}

/**
 * Production Expo wiring for Android SAF.
 *
 * A picked directory returns `content://` files. Android's Expo relocation API
 * cannot reliably copy a private cache file into those handles, so the
 * authenticated response is streamed directly into the owned SAF file.
 */
export function createExpoSessionFileDownloadBackend(): SessionFileDownloadBackend {
  if (Platform.OS === "android") {
    return createNativeAndroidDownloadBackend();
  }
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
