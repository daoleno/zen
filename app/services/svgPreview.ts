import { File } from "expo-file-system";
import { toByteArray } from "base64-js";
import type { SessionFileBinarySource } from "./sessionFilePreview";
import { MAX_SVG_BYTES, validateSvgPreview } from "./svgPreviewValidation";

async function readBounded(stream: ReadableStream<Uint8Array>, signal: AbortSignal) {
  const reader = stream.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    while (true) {
      if (signal.aborted) throw new Error("Image request cancelled.");
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > MAX_SVG_BYTES) throw new Error("SVG exceeds the preview limit.");
      chunks.push(value);
    }
  } finally {
    void reader.cancel().catch(() => {});
  }
  const bytes = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
}

export async function loadSvgPreview(source: SessionFileBinarySource, signal: AbortSignal) {
  if (signal.aborted) throw new Error("Image request cancelled.");
  let xml: string;
  if (/^data:image\/svg\+xml;base64,/i.test(source.uri)) {
    const encoded = source.uri.slice("data:image/svg+xml;base64,".length);
    if (encoded.length > Math.ceil(MAX_SVG_BYTES / 3) * 4 || !/^[\w+/=\r\n]+$/.test(encoded)) throw new Error("SVG exceeds the preview limit.");
    xml = new TextDecoder("utf-8", { fatal: true }).decode(toByteArray(encoded.replace(/\s/g, "")));
  } else if (/^content:/i.test(source.uri)) {
    const file = new File(source.uri);
    if (file.size > MAX_SVG_BYTES) throw new Error("SVG exceeds the preview limit.");
    const bytes = await file.bytes();
    if (bytes.byteLength > MAX_SVG_BYTES) throw new Error("SVG exceeds the preview limit.");
    xml = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } else if (/^file:/i.test(source.uri)) {
    const file = new File(source.uri);
    if (file.size > MAX_SVG_BYTES) throw new Error("SVG exceeds the preview limit.");
    xml = await readBounded(file.readableStream(), signal);
  } else {
    if (!/^https?:\/\//i.test(source.uri)) throw new Error("Unsupported SVG source.");
    const response = await fetch(source.uri, { headers: source.headers, signal, redirect: "error" });
    if (!response.ok || Number(response.headers.get("content-length")) > MAX_SVG_BYTES) throw new Error("SVG is unavailable or exceeds the preview limit.");
    if (!response.body) throw new Error("SVG is unavailable.");
    xml = await readBounded(response.body, signal);
  }
  if (signal.aborted) throw new Error("Image request cancelled.");
  return validateSvgPreview(xml);
}
