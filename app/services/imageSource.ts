import type { SessionFileBinarySource } from "./sessionFilePreview";

/** A phone grant is only accepted from the upload owner, never provider text. */
export type ZenImageSource =
  | { kind: "phone"; uri: string; name: string }
  | { kind: "external"; uri: string; name: string }
  | { kind: "inline"; uri: string; name: string }
  | { kind: "owned"; path: string; name: string };

export interface ZenImageOwner {
  key: string;
  resolve(path: string, signal: AbortSignal): Promise<SessionFileBinarySource>;
}

export function isImageAttachment(value: { mimeType?: string; name?: string; path?: string }) {
  return Boolean(value.mimeType?.startsWith("image/") || /\.(png|jpe?g|gif|webp|bmp|heic|heif|avif)(?:[?#].*)?$/i.test(value.name || value.path || ""));
}

export function imageReference(path: string, name = "Image"): ZenImageSource {
  if (path.startsWith("data:")) return { kind: "inline", uri: path, name };
  if (/^https?:\/\//i.test(path)) return { kind: "external", uri: path, name };
  return { kind: "owned", path, name };
}

export async function resolveImageSource(source: ZenImageSource, owner: ZenImageOwner | null, signal: AbortSignal): Promise<SessionFileBinarySource> {
  signal.throwIfAborted();
  if (source.kind === "owned") {
    if (!owner) throw new Error("Open this image from its Session or workspace.");
    const result = await owner.resolve(source.path, signal);
    signal.throwIfAborted();
    return result;
  }
  if (source.kind === "inline") {
    if (source.uri.length > 8 * 1024 * 1024 || !/^data:image\/(png|jpeg|gif|webp);base64,[A-Za-z0-9+/=\r\n]+$/.test(source.uri)) throw new Error("Inline image is unsupported or exceeds the preview limit.");
    return { uri: source.uri, headers: {} };
  }
  if (source.kind === "external") {
    const url = new URL(source.uri);
    if (!/^https?:$/.test(url.protocol) || url.username || url.password) throw new Error("Unsupported image URL.");
    // Native requests and redirects carry no Zen credentials.
    return { uri: url.href, headers: {} };
  }
  if (!/^(content:|file:)/i.test(source.uri)) throw new Error("The local image grant is unavailable.");
  return { uri: source.uri, headers: {} };
}

export function imageSourceKey(source: ZenImageSource, ownerKey: string) {
  return JSON.stringify([ownerKey, source.kind, source.kind === "owned" ? source.path : source.uri]);
}
