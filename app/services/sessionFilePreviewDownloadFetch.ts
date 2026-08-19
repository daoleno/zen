import type {
  SessionFileDownloadBackend,
  SessionFileDownloadDirectory,
  SessionFileOwnedDestination,
} from "./sessionFilePreviewDownload";
import { SESSION_FILE_BINARY_LIMIT_BYTES } from "./sessionFilePreviewDownload";
import {
  streamSessionFileDownloadToOwnedSink,
  type SessionFileDownloadFetch,
} from "./sessionFilePreviewDownloadStream";

export interface SessionFileCreateResult {
  readonly uri: string;
  delete(): void;
  writableStream(): WritableStream<Uint8Array>;
}

export interface SessionFileDirectoryApi {
  createFile(name: string, mimeType: string): SessionFileCreateResult;
}

export interface SessionFileFetchDownloadApi {
  pickDirectory(): Promise<SessionFileDirectoryApi>;
  fetch: SessionFileDownloadFetch;
}

function asOwnedDestination(
  file: SessionFileCreateResult,
): SessionFileOwnedDestination {
  return {
    get uri() {
      return file.uri;
    },
    delete() {
      file.delete();
    },
    writableStream() {
      return file.writableStream();
    },
  };
}

function asDownloadDirectory(
  directory: SessionFileDirectoryApi,
): SessionFileDownloadDirectory {
  return {
    reserve(fileName, mimeType) {
      return asOwnedDestination(directory.createFile(fileName, mimeType));
    },
  };
}

/**
 * Download backend that streams via fetch + owned writableStream.
 * Never calls File.downloadFileAsync (unsupported for Android SAF content://).
 */
export function createFetchSessionFileDownloadBackend(
  api: SessionFileFetchDownloadApi,
): SessionFileDownloadBackend {
  return {
    async pickDirectory() {
      return asDownloadDirectory(await api.pickDirectory());
    },
    async download(uri, destination, options) {
      await streamSessionFileDownloadToOwnedSink(
        uri,
        options.headers,
        destination,
        {
          fetch: api.fetch,
          maxBytes: SESSION_FILE_BINARY_LIMIT_BYTES,
          expectedBytes: options.expectedBytes,
        },
      );
    },
  };
}
