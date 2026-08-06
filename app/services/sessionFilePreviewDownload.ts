import type {
  SessionFileBinaryRequest,
  SessionFileBinarySource,
  SessionFileMetadata,
} from "./sessionFilePreview";

/** Matches daemon `maxSessionFileBinaryBytes` (50 MiB). */
export const SESSION_FILE_BINARY_LIMIT_BYTES = 50 * 1024 * 1024;

export type SessionFileDownloadResult = "saved" | "cancelled";

/**
 * A destination file this attempt created and therefore owns.
 * Cleanup may delete only this handle — never an unclaimed path.
 */
export interface SessionFileOwnedDestination {
  readonly uri: string;
  delete(): void;
  writableStream(): WritableStream<Uint8Array>;
}

export interface SessionFileDownloadDirectory {
  /**
   * Atomically create and own an empty file with this display name.
   * Must use Directory.createFile on Expo (not File.create — unsupported on
   * Android SAF content://). Throws on explicit name conflict where the
   * platform rejects; on SAF a provider may uniquify and still return a
   * distinct owned URI.
   */
  reserve(fileName: string, mimeType: string): SessionFileOwnedDestination;
}

export interface SessionFileDownloadBackend {
  pickDirectory(): Promise<SessionFileDownloadDirectory>;
  /**
   * Stream the authenticated source into an already-reserved owned file.
   */
  download(
    uri: string,
    destination: SessionFileOwnedDestination,
    options: { headers: Record<string, string> },
  ): Promise<void>;
}

export function sessionFileDownloadFileName(
  metadata: Pick<SessionFileMetadata, "name" | "path">,
): string {
  const raw =
    metadata.name.trim() ||
    metadata.path.replace(/\\/g, "/").split("/").at(-1)?.trim() ||
    "file";
  let sanitized = raw.replace(/[/\\]/g, "_").replace(/\u0000/g, "").trim();
  if (
    !sanitized ||
    sanitized === "." ||
    sanitized === ".." ||
    /^\.+$/.test(sanitized)
  ) {
    sanitized = "file";
  }
  return sanitized.slice(0, 255) || "file";
}

/** Prefer metadata content-type without parameters; else octet-stream. */
export function sessionFileDownloadMimeType(
  contentType: string | null | undefined,
): string {
  const base = contentType?.split(";")[0]?.trim() || "";
  return base || "application/octet-stream";
}

export function sessionFileCanDownload(
  metadata: SessionFileMetadata | null | undefined,
): boolean {
  if (!metadata) return false;
  if (metadata.tooLarge) return false;
  if (!metadata.path.trim() || !metadata.generation.trim()) return false;
  if (!Number.isFinite(metadata.size) || metadata.size < 0) return false;
  return metadata.size <= SESSION_FILE_BINARY_LIMIT_BYTES;
}

export function sessionFileDownloadRequest(
  identity: {
    agentId: string;
    processId: number;
    startedAt: number;
  },
  metadata: SessionFileMetadata,
): SessionFileBinaryRequest {
  return {
    agentId: identity.agentId,
    processId: identity.processId,
    startedAt: identity.startedAt,
    path: metadata.path,
    generation: metadata.generation,
  };
}

/** User/picker/abort cancellation only — never HTTP failure text. */
export function isSessionFileDownloadCancelError(error: unknown): boolean {
  const message =
    error instanceof Error
      ? error.message
      : typeof error === "string"
        ? error
        : "";
  return (
    /(?:picker|picking)\s+was\s+cancelled/i.test(message) ||
    /download was cancelled/i.test(message) ||
    /operation was aborted/i.test(message)
  );
}

/**
 * Explicit name-conflict messages from Expo native create paths.
 * Does not treat generic "could not be created" as collision.
 */
export function isSessionFileDownloadReserveConflictError(
  error: unknown,
): boolean {
  const message =
    error instanceof Error
      ? error.message
      : typeof error === "string"
        ? error
        : "";
  if (/could not be created/i.test(message) && !/already exists/i.test(message)) {
    return false;
  }
  return (
    /\bit already exists\b/i.test(message) ||
    /file already exists/i.test(message) ||
    /destination already exists/i.test(message) ||
    /same name already exists/i.test(message) ||
    /\bEEXIST\b/.test(message)
  );
}

export function splitSessionFileDownloadName(fileName: string): {
  stem: string;
  extension: string;
} {
  const trimmed = fileName.trim() || "file";
  const lastDot = trimmed.lastIndexOf(".");
  if (lastDot <= 0 || lastDot === trimmed.length - 1) {
    return { stem: trimmed, extension: "" };
  }
  return {
    stem: trimmed.slice(0, lastDot),
    extension: trimmed.slice(lastDot),
  };
}

export function sessionFileDownloadNameCandidates(
  preferredName: string,
  maxSuffix = 32,
): string[] {
  const names = [preferredName];
  const { stem, extension } = splitSessionFileDownloadName(preferredName);
  for (let index = 1; index <= maxSuffix; index += 1) {
    names.push(`${stem} (${index})${extension}`);
  }
  return names;
}

/**
 * Reserve by attempting create/own for preferred name then collision suffixes.
 * Only explicit already-exists conflicts retry; generic create failures surface.
 * Android SAF uniquify success returns the owned URI without suffix churn.
 */
export function reserveCollisionSafeDownloadDestination(
  directory: SessionFileDownloadDirectory,
  preferredName: string,
  mimeType: string,
  maxSuffix = 32,
): { fileName: string; destination: SessionFileOwnedDestination } {
  let lastError: unknown;
  for (const fileName of sessionFileDownloadNameCandidates(
    preferredName,
    maxSuffix,
  )) {
    try {
      const destination = directory.reserve(fileName, mimeType);
      return { fileName, destination };
    } catch (error) {
      lastError = error;
      if (!isSessionFileDownloadReserveConflictError(error)) {
        throw error;
      }
    }
  }
  throw lastError instanceof Error
    ? lastError
    : new Error("Could not reserve an unused download file name.");
}

function deleteOwnedDestination(destination: SessionFileOwnedDestination) {
  try {
    destination.delete();
  } catch {
    // Best-effort cleanup of our reserved handle only.
  }
}

export async function exportSessionFileDownload(input: {
  fileName: string;
  mimeType: string;
  resolveSource(): Promise<SessionFileBinarySource>;
  backend: SessionFileDownloadBackend;
}): Promise<SessionFileDownloadResult> {
  let directory: SessionFileDownloadDirectory;
  try {
    directory = await input.backend.pickDirectory();
  } catch (error) {
    if (isSessionFileDownloadCancelError(error)) {
      return "cancelled";
    }
    throw error;
  }

  const { destination: owned } = reserveCollisionSafeDownloadDestination(
    directory,
    input.fileName,
    input.mimeType,
  );

  try {
    const source = await input.resolveSource();
    await input.backend.download(source.uri, owned, {
      headers: source.headers,
    });
    return "saved";
  } catch (error) {
    // Delete only the handle this attempt reserved — never an unclaimed path.
    deleteOwnedDestination(owned);
    if (isSessionFileDownloadCancelError(error)) {
      return "cancelled";
    }
    throw error;
  }
}
