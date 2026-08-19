import { SESSION_FILE_BINARY_LIMIT_BYTES } from "./sessionFilePreviewDownload";

export interface SessionFileDownloadFetchResponse {
  readonly ok: boolean;
  readonly status: number;
  readonly body: ReadableStream<Uint8Array> | null;
}

export interface SessionFileDownloadFetch {
  (
    url: string,
    init: { method: "GET"; headers: Record<string, string> },
  ): Promise<SessionFileDownloadFetchResponse>;
}

export interface SessionFileDownloadStreamSink {
  writableStream(): WritableStream<Uint8Array>;
}

export interface StreamSessionFileDownloadOptions {
  fetch: SessionFileDownloadFetch;
  maxBytes?: number;
  expectedBytes?: number;
}

async function cancelBodyQuietly(
  body: ReadableStream<Uint8Array> | null | undefined,
) {
  if (!body) return;
  try {
    await body.cancel();
  } catch {
    // Best-effort release of the HTTP body.
  }
}

/**
 * Stream an authenticated Session-file GET into an already-reserved owned sink.
 * Never buffers the full body in JS and never uses downloadFileAsync.
 */
export async function streamSessionFileDownloadToOwnedSink(
  sourceUri: string,
  headers: Record<string, string>,
  sink: SessionFileDownloadStreamSink,
  options: StreamSessionFileDownloadOptions,
): Promise<void> {
  const maxBytes = options.maxBytes ?? SESSION_FILE_BINARY_LIMIT_BYTES;
  const response = await options.fetch(sourceUri, {
    method: "GET",
    headers,
  });

  if (!response.ok) {
    await cancelBodyQuietly(response.body);
    throw new Error(
      `Session file download failed (HTTP ${response.status}).`,
    );
  }

  if (!response.body) {
    throw new Error("Session file download returned an empty body.");
  }

  let bytesWritten = 0;
  const limit = new TransformStream<Uint8Array, Uint8Array>({
    transform(chunk, controller) {
      bytesWritten += chunk.byteLength;
      if (bytesWritten > maxBytes) {
        controller.error(
          new Error(
            `Session file download exceeded the ${maxBytes} byte preview limit.`,
          ),
        );
        return;
      }
      controller.enqueue(chunk);
    },
  });

  const writable = sink.writableStream();
  try {
    await response.body.pipeThrough(limit).pipeTo(writable);
    if (
      options.expectedBytes !== undefined &&
      bytesWritten !== options.expectedBytes
    ) {
      throw new Error(
        `Session file download was truncated (expected ${options.expectedBytes} bytes, received ${bytesWritten}).`,
      );
    }
  } catch (error) {
    try {
      await writable.abort?.(
        error instanceof Error ? error : new Error(String(error)),
      );
    } catch {
      // Writable may already be closed by pipeTo.
    }
    throw error instanceof Error
      ? error
      : new Error("Session file download stream failed.");
  }
}
