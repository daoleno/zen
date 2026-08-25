import * as DocumentPicker from "expo-document-picker";
import {
  File,
  type UploadProgress as NativeUploadProgress,
} from "expo-file-system";

const BINARY_UPLOAD_TYPE = 0;
import { buildAuthorizationHeader } from "./auth";
import { getServerById, type StoredServer } from "./storage";
import { resolveCanonicalServerURL } from "./pinnedTransport";

export type UploadedAttachment = {
  name: string;
  path: string;
  /** Local file URI for composer/timeline thumbnails before/after upload. */
  localUri?: string;
  mimeType?: string;
};

export type UploadDocumentAsset = Pick<
  DocumentPicker.DocumentPickerAsset,
  "uri" | "name" | "mimeType" | "size"
>;

export type UploadProgressSnapshot = {
  transferredBytes: number | null;
  totalBytes: number | null;
  fraction: number | null;
  bytesPerSecond?: number;
  etaSeconds?: number;
};

export type ActiveAttachmentUpload = {
  name: string;
  progress: UploadProgressSnapshot | null;
};

export interface AttachmentUploadOperation {
  readonly result: Promise<UploadedAttachment>;
  /** Returns a genuine native cancellation error, if cancellation itself fails. */
  cancel(): Error | null;
}

export interface AttachmentUploadOperationOptions {
  onProgress?(progress: UploadProgressSnapshot): void;
  now?(): number;
}

export class AttachmentUploadCancelledError extends Error {
  constructor() {
    super("Attachment upload cancelled.");
    this.name = "AttachmentUploadCancelledError";
  }
}

// Mirrors the daemon's explicit V1 file limit to avoid starting a transfer the
// server cannot accept. The daemon still enforces its policy authoritatively.
export const V1_MAX_UPLOAD_FILE_BYTES = 2 * 1024 * 1024 * 1024;
const UPLOAD_NAME_HEADER = "X-Zen-Upload-Name";
const MAX_UPLOAD_NAME_BYTES = 1024;
const MAX_UPLOAD_NAME_HEADER_BYTES = 3 * 1024;

export async function buildUploadHeaders(
  daemonId: string,
): Promise<Record<string, string>> {
  return {
    Authorization: await buildAuthorizationHeader({
      daemonId,
      purpose: "zen-upload",
    }),
  };
}

export function buildUploadUrl(serverUrl: string): string | null {
  if (!serverUrl) {
    return null;
  }

  try {
    const url = new URL(serverUrl);
    if (url.protocol === "ws:") {
      url.protocol = "http:";
    } else if (url.protocol === "wss:") {
      url.protocol = "https:";
    }
    url.pathname = "/upload";
    url.search = "";
    url.hash = "";
    return url.toString();
  } catch {
    return null;
  }
}

export function createAttachmentUploadOperation(
  asset: UploadDocumentAsset,
  server: StoredServer,
  options: AttachmentUploadOperationOptions = {},
): AttachmentUploadOperation {
  if (!buildUploadUrl(server.url)) {
    throw new Error("Server URL is not configured.");
  }
  if (typeof asset.size === "number" && asset.size > V1_MAX_UPLOAD_FILE_BYTES) {
    throw new Error("File exceeds the 2 GiB upload limit.");
  }

  const file = new File(asset.uri);
  const originalName = asset.name || file.name || "upload";
  const contentType = asset.mimeType || file.type || "application/octet-stream";
  const encodedName = encodeUploadName(originalName);
  let task: ReturnType<typeof file.createUploadTask> | null = null;
  let cancelRequested = false;
  let settled = false;
  let cancelFailure: Error | null = null;
  let projectedProgress: UploadProgressSnapshot | null = null;
  let uploadStartedAt = 0;

  const result = (async (): Promise<UploadedAttachment> => {
    try {
      const headers = await buildUploadHeaders(server.daemonId);
      const transportURL = await resolveCanonicalServerURL(server);
      const uploadUrl = buildUploadUrl(transportURL);
      if (!uploadUrl) {
        throw new Error("Server URL is not configured.");
      }
      if (cancelRequested) {
        throw cancelFailure ?? new AttachmentUploadCancelledError();
      }

      task = file.createUploadTask(uploadUrl, {
        httpMethod: "POST",
        uploadType: BINARY_UPLOAD_TYPE,
        headers: {
          ...headers,
          "Content-Type": contentType,
          [UPLOAD_NAME_HEADER]: encodedName,
        },
        onProgress(nativeProgress) {
          if (cancelRequested || settled) {
            return;
          }
          projectedProgress = projectUploadProgress(
            projectedProgress,
            nativeProgress,
          );
          projectedProgress = projectUploadTiming(
            projectedProgress,
            (options.now?.() ?? Date.now()) - uploadStartedAt,
          );
          options.onProgress?.(projectedProgress);
        },
      });

      uploadStartedAt = options.now?.() ?? Date.now();
      const uploadResult = await task.uploadAsync();
      if (cancelRequested) {
        throw cancelFailure ?? new AttachmentUploadCancelledError();
      }
      if (uploadResult.status < 200 || uploadResult.status >= 300) {
        throw new Error(
          uploadResult.body.trim() || `Upload failed (${uploadResult.status})`,
        );
      }

      let payload: { path?: string; name?: string };
      try {
        payload = JSON.parse(uploadResult.body) as {
          path?: string;
          name?: string;
        };
      } catch {
        throw new Error("Upload returned an invalid response.");
      }
      if (!payload.path) {
        throw new Error("Upload response missing file path.");
      }

      return {
        name: payload.name || originalName,
        path: payload.path,
        localUri: asset.uri,
        mimeType: asset.mimeType || undefined,
      };
    } catch (error) {
      if (cancelRequested && !cancelFailure) {
        throw new AttachmentUploadCancelledError();
      }
      throw error;
    } finally {
      settled = true;
      task?.release();
    }
  })();

  return {
    result,
    cancel() {
      if (settled || cancelRequested) {
        return null;
      }
      cancelRequested = true;
      if (!task) {
        return null;
      }
      try {
        task.cancel();
        return null;
      } catch (error) {
        cancelFailure = normalizeError(error, "Could not cancel this upload.");
        return cancelFailure;
      }
    },
  };
}

export async function uploadDocumentAsset(
  asset: UploadDocumentAsset,
  server: StoredServer,
  options: AttachmentUploadOperationOptions = {},
): Promise<UploadedAttachment> {
  return createAttachmentUploadOperation(asset, server, options).result;
}

export function projectUploadProgress(
  previous: UploadProgressSnapshot | null,
  nativeProgress: NativeUploadProgress,
): UploadProgressSnapshot {
  const nativeTransferred = normalizeByteCount(nativeProgress.bytesSent);
  const transferredBytes = maxKnownByteCount(
    previous?.transferredBytes ?? null,
    nativeTransferred,
  );
  const nativeTotal = normalizePositiveByteCount(nativeProgress.totalBytes);
  const totalBytes =
    nativeTotal ??
    (previous?.totalBytes !== null && previous?.totalBytes !== undefined
      ? previous.totalBytes
      : null);
  const nativeFraction =
    transferredBytes !== null && totalBytes !== null
      ? clampFraction(transferredBytes / totalBytes)
      : null;
  const fraction =
    nativeFraction === null
      ? null
      : Math.max(previous?.fraction ?? 0, nativeFraction);

  return { transferredBytes, totalBytes, fraction };
}

function projectUploadTiming(
  progress: UploadProgressSnapshot,
  elapsedMilliseconds: number,
): UploadProgressSnapshot {
  if (
    elapsedMilliseconds < 250 ||
    progress.transferredBytes === null ||
    progress.transferredBytes <= 0
  ) {
    return progress;
  }
  const bytesPerSecond = Math.round(
    progress.transferredBytes / (elapsedMilliseconds / 1000),
  );
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) {
    return progress;
  }
  if (progress.totalBytes === null) {
    return { ...progress, bytesPerSecond };
  }
  const remainingBytes = Math.max(
    0,
    progress.totalBytes - progress.transferredBytes,
  );
  return {
    ...progress,
    bytesPerSecond,
    etaSeconds: Math.ceil(remainingBytes / bytesPerSecond),
  };
}

export async function pickUploadDocument(): Promise<UploadDocumentAsset | null> {
  const result = await DocumentPicker.getDocumentAsync({
    type: ["*/*"],
    copyToCacheDirectory: false,
  });
  if (result.canceled || !result.assets?.length) {
    return null;
  }
  return result.assets[0];
}

export async function resolveServerUploadTarget(
  serverId: string,
): Promise<StoredServer> {
  const server = await getServerById(serverId);
  if (!server) {
    throw new Error("Server not found.");
  }
  return server;
}

function encodeUploadName(name: string): string {
  let encoded: string;
  try {
    encoded = encodeURIComponent(name);
  } catch {
    throw new Error("File name cannot be uploaded.");
  }
  if (
    !encoded ||
    encoded.length > MAX_UPLOAD_NAME_HEADER_BYTES ||
    encodedByteLength(encoded) > MAX_UPLOAD_NAME_BYTES
  ) {
    throw new Error("File name is too long.");
  }
  return encoded;
}

function encodedByteLength(encoded: string): number {
  let length = 0;
  for (let index = 0; index < encoded.length; index += 1) {
    length += 1;
    if (encoded[index] === "%") {
      index += 2;
    }
  }
  return length;
}

export async function uploadDocumentForServer(
  serverId: string,
): Promise<UploadedAttachment | null> {
  const asset = await pickUploadDocument();
  if (!asset) {
    return null;
  }
  const target = await resolveServerUploadTarget(serverId);
  return uploadDocumentAsset(asset, target);
}

function normalizeByteCount(value: number): number | null {
  return Number.isFinite(value) && value >= 0 ? Math.floor(value) : null;
}

function normalizePositiveByteCount(value: number): number | null {
  return Number.isFinite(value) && value > 0 ? Math.floor(value) : null;
}

function maxKnownByteCount(
  previous: number | null,
  next: number | null,
): number | null {
  if (previous === null) return next;
  if (next === null) return previous;
  return Math.max(previous, next);
}

function clampFraction(value: number) {
  return Math.max(0, Math.min(1, value));
}

function normalizeError(error: unknown, fallback: string) {
  return error instanceof Error ? error : new Error(fallback);
}
