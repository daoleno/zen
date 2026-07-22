export interface SessionPdfStagingFile {
  readonly uri: string;
  readonly size: number | null;
  readonly exists: boolean;
  delete(): void;
}

export interface SessionPdfDownloadOptions {
  headers: Record<string, string>;
  signal: AbortSignal;
  onProgress(progress: { bytesWritten: number; totalBytes: number }): void;
}

export interface SessionPdfStagingBackend {
  createTarget(name: string): SessionPdfStagingFile;
  download(
    uri: string,
    target: SessionPdfStagingFile,
    options: SessionPdfDownloadOptions,
  ): Promise<SessionPdfStagingFile>;
}

export interface SessionPdfStageInput {
  uri: string;
  headers: Record<string, string>;
  generation: string;
  expectedBytes: number;
  owner: string;
  epoch: number;
}

export class SessionPdfStageError extends Error {
  constructor(
    message: string,
    readonly stale: boolean,
    readonly cancelled = false,
  ) {
    super(message);
    this.name = "SessionPdfStageError";
  }
}

export interface SessionPdfStageOperation {
  readonly result: Promise<string>;
  dispose(): void;
}

export function createSessionPdfStage(
  input: SessionPdfStageInput,
  backend: SessionPdfStagingBackend,
): SessionPdfStageOperation {
  const abortController = new AbortController();
  const target = backend.createTarget(
    sessionPdfStageFileName(input.generation, input.owner, input.epoch),
  );
  let stagedFile = target;
  let disposed = false;
  let boundaryError: SessionPdfStageError | null = null;

  deleteStagedPdf(target);

  const cleanup = () => {
    deleteStagedPdf(target);
    if (stagedFile !== target) deleteStagedPdf(stagedFile);
  };

  const result = backend
    .download(input.uri, target, {
      headers: input.headers,
      signal: abortController.signal,
      onProgress(progress) {
        if (
          input.expectedBytes >= 0 &&
          (progress.bytesWritten > input.expectedBytes ||
            (progress.totalBytes >= 0 &&
              progress.totalBytes > input.expectedBytes))
        ) {
          boundaryError = new SessionPdfStageError(
            "The PDF stream exceeded its inspected file size.",
            true,
          );
          abortController.abort(boundaryError.message);
        }
      },
    })
    .then((downloadedFile) => {
      stagedFile = downloadedFile;
      if (disposed) {
        throw new SessionPdfStageError("PDF preview closed.", false, true);
      }
      if (boundaryError) throw boundaryError;
      if (
        input.expectedBytes >= 0 &&
        typeof downloadedFile.size === "number" &&
        downloadedFile.size !== input.expectedBytes
      ) {
        throw new SessionPdfStageError(
          "The PDF changed while it was being downloaded.",
          true,
        );
      }
      return downloadedFile.uri;
    })
    .catch((error) => {
      cleanup();
      if (disposed) {
        throw new SessionPdfStageError("PDF preview closed.", false, true);
      }
      if (boundaryError) throw boundaryError;
      if (error instanceof SessionPdfStageError) throw error;
      const message =
        error instanceof Error ? error.message : "Could not download the PDF.";
      const stale = /(?:^|\D)409(?:\D|$)/.test(message);
      throw new SessionPdfStageError(
        stale
          ? "The PDF changed before it could be opened."
          : "Could not download the PDF into the secure preview.",
        stale,
      );
    });

  return {
    result,
    dispose() {
      if (disposed) return;
      disposed = true;
      abortController.abort("PDF preview closed");
      cleanup();
    },
  };
}

export function sessionPdfStageFileName(
  generation: string,
  owner: string,
  epoch: number,
): string {
  const safeGeneration = safeFileToken(generation, 64) || "file";
  const safeOwner = safeFileToken(owner, 32) || "preview";
  const safeEpoch = Number.isSafeInteger(epoch) && epoch > 0 ? epoch : 1;
  return `zen-session-pdf-${safeGeneration}-${safeOwner}-${safeEpoch}.pdf`;
}

function safeFileToken(value: string, maxLength: number): string {
  return value.replace(/[^a-z0-9]/gi, "").slice(0, maxLength);
}

function deleteStagedPdf(file: SessionPdfStagingFile) {
  try {
    if (file.exists) file.delete();
  } catch {
    // A cancelled native download can release its file just after dispose.
    // The rejected download path retries cleanup without retaining the error.
  }
}
