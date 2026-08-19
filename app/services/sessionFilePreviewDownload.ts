import type {
  SessionFileBinaryRequest,
  SessionFileBinarySource,
  SessionFileMetadata,
} from "./sessionFilePreview";

/** Matches daemon `maxSessionFileBinaryBytes` (50 MiB). */
export const SESSION_FILE_BINARY_LIMIT_BYTES = 50 * 1024 * 1024;

export type SessionFileDownloadResult = "saved" | "cancelled";
export type SessionFileDownloadFeedback = "idle" | "busy" | "saved" | "failed";

export function sessionFileDownloadErrorMessage(error: unknown): string {
  const message =
    error instanceof Error
      ? error.message
      : typeof error === "string"
        ? error
        : "Unknown download error.";
  const normalized = message.trim();
  return normalized || "Unknown download error.";
}

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
    options: { headers: Record<string, string>; expectedBytes?: number },
  ): Promise<void>;
}

export interface SessionFileDownloadLifecycleOwner {
  start(task: () => Promise<SessionFileDownloadResult>): Promise<SessionFileDownloadResult | undefined>;
  reset(): void;
  dispose(): void;
}

export function createSessionFileDownloadLifecycleOwner(input: {
  onFeedbackChange(
    feedback: SessionFileDownloadFeedback,
    error?: unknown,
  ): void;
}): SessionFileDownloadLifecycleOwner {
  let disposed = false;
  let active = false;
  let epoch = 0;

  return {
    async start(task) {
      if (disposed || active) return undefined;
      active = true;
      const taskEpoch = ++epoch;
      input.onFeedbackChange("busy");
      try {
        const result = await task();
        if (!disposed && epoch === taskEpoch) {
          input.onFeedbackChange(result === "saved" ? "saved" : "idle");
        }
        return result;
      } catch (error) {
        if (!disposed && epoch === taskEpoch) {
          input.onFeedbackChange("failed", error);
        }
        throw error;
      } finally {
        active = false;
      }
    },
    reset() {
      epoch += 1;
      if (!disposed) input.onFeedbackChange("idle");
    },
    dispose() {
      disposed = true;
      epoch += 1;
      active = false;
    },
  };
}

export function sessionFileDownloadFileName(
  metadata: Pick<SessionFileMetadata, "name" | "path">,
): string {
  const pathName = metadata.path.replace(/\\/g, "/").split("/").at(-1)?.trim() || "";
  const raw = metadata.name.trim() || pathName || "file";
  let sanitized = raw.replace(/[/\\]/g, "_").replace(/[\u0000-\u001f\u007f]/g, "").trim();
  const pathExtension = splitSessionFileDownloadName(sanitizeSessionFileName(pathName)).extension;
  const { stem, extension } = splitSessionFileDownloadName(sanitized);
  const cleanedStem = stripSessionFileImportNoise(stem);
  sanitized = `${cleanedStem || stem}${extension || pathExtension}`;
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

const SESSION_FILE_SOURCE_SUFFIXES = [
  "airtable",
  "box",
  "confluence",
  "databricks",
  "dropbox",
  "github",
  "gitlab",
  "google drive",
  "google docs",
  "jira",
  "linear",
  "microsoft 365",
  "notion",
  "onedrive",
  "sharepoint",
  "slack",
].map((value) => value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));

const SESSION_FILE_SOURCE_SUFFIX_RE = new RegExp(
  `(?:${SESSION_FILE_SOURCE_SUFFIXES.join("|")})(?:\\.com)?`,
  "i",
);
const SESSION_FILE_DOMAIN_SUFFIX_RE = /(?:https?:\/\/|www\.)?[a-z0-9-]+(?:\.[a-z0-9-]+)+/i;

function sanitizeSessionFileName(value: string): string {
  return value.replace(/[/\\]/g, "_").replace(/[\u0000-\u001f\u007f]/g, "").trim();
}

function stripSessionFileImportNoise(stem: string): string {
  let cleaned = stem.trim();
  let previous = "";
  while (cleaned && cleaned !== previous) {
    previous = cleaned;
    cleaned = cleaned
      .replace(/\s*(?:[-–—|·•]|\b(?:from|on)\b)\s*\([^)]*\)\s*$/i, "")
      .replace(
        new RegExp(
          `\\s*(?:[-–—|·•]|\\b(?:from|on)\\b)\\s*(?:${SESSION_FILE_SOURCE_SUFFIX_RE.source}|${SESSION_FILE_DOMAIN_SUFFIX_RE.source})\\s*$`,
          "i",
        ),
        "",
      )
      .replace(/\s*[([]\s*(?:imported|exported|downloaded|uploaded)\s*[)\]]\s*$/i, "")
      .replace(/\s*[-–—|·•]\s*(?:imported|exported|downloaded|uploaded|attachment|file)\s*$/i, "")
      .trim();
  }
  return cleaned;
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
  expectedBytes?: number;
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
      expectedBytes: input.expectedBytes,
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
