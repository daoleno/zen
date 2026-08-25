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

interface ZenFileUploadNativeModule {
  upload(request: NativeUploadRequest): Promise<NativeUploadResult>;
  cancel(uploadId: string): boolean;
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
