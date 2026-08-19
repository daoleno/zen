import { Directory, File, Paths } from "expo-file-system";
import type { SessionFileDownloadBackend } from "./sessionFilePreviewDownload";

let temporaryFileSequence = 0;

function createTemporaryDownloadFile(): File {
  temporaryFileSequence += 1;
  return new File(
    Paths.cache,
    `.zen-session-download-${Date.now().toString(36)}-${temporaryFileSequence.toString(36)}-${Math.random().toString(36).slice(2)}`,
  );
}

/**
 * Production Expo wiring: reserve through the system directory picker, download
 * into an app-private cache file, then copy into the reserved destination.
 *
 * The cache staging is intentional. Expo SDK 57's `expo/fetch` native response
 * streaming path can tear down a debug client while its first native stream is
 * being connected; `File.downloadFileAsync` keeps the network transfer on the
 * native file-download path and still supports authenticated headers.
 */
export function createExpoSessionFileDownloadBackend(): SessionFileDownloadBackend {
  const reservedFiles = new Map<string, File>();

  return {
    async pickDirectory() {
      const directory = await Directory.pickDirectoryAsync();
      return {
        reserve(name, mimeType) {
          // Android SAF content:// requires Directory.createFile — File.create throws.
          const file = directory.createFile(name, mimeType);
          reservedFiles.set(file.uri, file);
          return {
            get uri() {
              return file.uri;
            },
            delete() {
              reservedFiles.delete(file.uri);
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
    async download(uri, destination, options) {
      const target = reservedFiles.get(destination.uri);
      if (!target) {
        throw new Error("The download destination is no longer owned.");
      }
      const temporary = createTemporaryDownloadFile();
      try {
        const downloaded = await File.downloadFileAsync(uri, temporary, options);
        if (
          options.expectedBytes !== undefined &&
          (downloaded.size ?? 0) !== options.expectedBytes
        ) {
          throw new Error(
            `Session file download was truncated (expected ${options.expectedBytes} bytes, received ${downloaded.size ?? 0}).`,
          );
        }
        await downloaded.copy(target, { overwrite: true });
      } finally {
        if (temporary.exists) {
          temporary.delete();
        }
      }
    },
  };
}
