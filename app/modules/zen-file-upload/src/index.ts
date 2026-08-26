import { requireNativeModule, type EventSubscription } from "expo-modules-core";

export interface NativeUploadProgress {
  uploadId: string;
  bytesSent: number;
  totalBytes: number;
}

export interface NativeUploadResult {
  body: string;
  status: number;
  headers: Record<string, string>;
}

export interface NativeUploadRequest {
  uploadId: string;
  url: string;
  fileUri: string;
  expectedSize: number | null;
  method: string;
  headers: Record<string, string>;
}

export interface NativeDownloadResult {
  bytesWritten: number;
}

export interface NativeDownloadRequest {
  downloadId: string;
  url: string;
  destinationUri: string;
  expectedSize: number | null;
  maxBytes: number;
  headers: Record<string, string>;
}

interface ZenFileUploadNativeModule {
  upload(request: NativeUploadRequest): Promise<NativeUploadResult>;
  cancel(uploadId: string): boolean;
  download?(request: NativeDownloadRequest): Promise<NativeDownloadResult>;
  cancelDownload?(downloadId: string): boolean;
  addListener(
    eventName: "onUploadProgress",
    listener: (progress: NativeUploadProgress) => void,
  ): EventSubscription;
}

let cached: ZenFileUploadNativeModule | null | undefined;

export function getZenFileUploadModule(): ZenFileUploadNativeModule | null {
  if (cached === undefined) {
    try {
      cached = requireNativeModule<ZenFileUploadNativeModule>("ZenFileUpload");
    } catch {
      cached = null;
    }
  }
  return cached;
}

export function getZenFileDownloadModule(): ZenFileUploadNativeModule | null {
  const module = getZenFileUploadModule();
  return module && typeof module.download === "function" ? module : null;
}
