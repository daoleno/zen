import type { SessionFileBinarySource } from "./sessionFilePreview";

/** A phone grant is only accepted from the upload owner, never provider text. */
export type ZenImageSource =
  | { kind: "phone"; uri: string; name: string; mimeType?: string }
  | { kind: "external"; uri: string; name: string; mimeType?: string }
  | { kind: "inline"; uri: string; name: string; mimeType?: string }
  | { kind: "owned"; path: string; name: string; mimeType?: string };

export interface ZenImageOwner {
  key: string;
  resolve(path: string, signal: AbortSignal): Promise<SessionFileBinarySource>;
}

export function isImageAttachment(value: { mimeType?: string; name?: string; path?: string }) {
  return Boolean(value.mimeType?.startsWith("image/") || /\.(png|jpe?g|gif|webp|bmp|heic|heif|avif|svg)(?:[?#].*)?$/i.test(value.name || value.path || ""));
}

export function isSvgImage(source: ZenImageSource, resolved?: SessionFileBinarySource) {
  if ([source.mimeType, resolved?.mimeType].some((type) => type?.toLowerCase().split(";", 1)[0] === "image/svg+xml")) return true;
  if (/\.svg(?:[?#].*)?$/i.test(source.name) || (source.kind === "owned" && /\.svg(?:[?#].*)?$/i.test(source.path))) return true;
  const uri = resolved?.uri || (source.kind === "owned" ? "" : source.uri);
  return /^data:image\/svg\+xml(?:;|,)/i.test(uri) || /\.svg(?:[?#].*)?$/i.test(uri);
}

export function imageReference(path: string, name = "Image", mimeType?: string): ZenImageSource {
  if (path.startsWith("data:")) return { kind: "inline", uri: path, name, mimeType };
  if (/^https?:\/\//i.test(path)) return { kind: "external", uri: path, name, mimeType };
  return { kind: "owned", path, name, mimeType };
}

export async function resolveImageSource(source: ZenImageSource, owner: ZenImageOwner | null, signal: AbortSignal): Promise<SessionFileBinarySource> {
  if (signal.aborted) throw new Error("Image request cancelled.");
  if (source.kind === "owned") {
    if (!owner) throw new Error("Open this image from its Session or workspace.");
    const result = await owner.resolve(source.path, signal);
    if (signal.aborted) throw new Error("Image request cancelled.");
    return result;
  }
  if (source.kind === "inline") {
    if (source.uri.length > 8 * 1024 * 1024 || !/^data:image\/(?:png|jpeg|gif|webp|svg\+xml);base64,[A-Za-z0-9+/=\r\n]+$/i.test(source.uri)) throw new Error("Inline image is unsupported or exceeds the preview limit.");
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
