import {
  MERMAID_MAX_POST_MESSAGE_BYTES,
  MERMAID_MAX_SVG_BYTES,
  MERMAID_MESSAGE_VERSION,
  byteLength,
} from "./mermaidLimits";
import { sanitizeMermaidSvg } from "./mermaidSecurity";

export type MermaidHostMessage =
  | {
      v: typeof MERMAID_MESSAGE_VERSION;
      type: "ready";
    }
  | {
      v: typeof MERMAID_MESSAGE_VERSION;
      type: "result";
      requestId: string;
      generation: number;
      ok: true;
      svg: string;
      width: number;
      height: number;
    }
  | {
      v: typeof MERMAID_MESSAGE_VERSION;
      type: "result";
      requestId: string;
      generation: number;
      ok: false;
      error: string;
    };

export type MermaidRenderRequest = {
  v: typeof MERMAID_MESSAGE_VERSION;
  type: "render";
  requestId: string;
  generation: number;
  source: string;
  theme: Record<string, string>;
  timeoutMs: number;
};

const REQUEST_ID_PATTERN = /^[a-zA-Z0-9:_-]{1,80}$/;

export function parseMermaidHostMessage(raw: string): MermaidHostMessage | null {
  if (byteLength(raw) > MERMAID_MAX_POST_MESSAGE_BYTES) {
    return null;
  }
  let payload: unknown;
  try {
    payload = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!payload || typeof payload !== "object") {
    return null;
  }
  const message = payload as Partial<MermaidHostMessage> & {
    svg?: unknown;
    width?: unknown;
    height?: unknown;
    error?: unknown;
    requestId?: unknown;
    generation?: unknown;
    ok?: unknown;
  };
  if (message.v !== MERMAID_MESSAGE_VERSION) {
    return null;
  }
  if (message.type === "ready") {
    return { v: MERMAID_MESSAGE_VERSION, type: "ready" };
  }
  if (message.type !== "result" || typeof message.requestId !== "string") {
    return null;
  }
  if (!REQUEST_ID_PATTERN.test(message.requestId)) {
    return null;
  }
  if (
    typeof message.generation !== "number" ||
    !Number.isSafeInteger(message.generation) ||
    message.generation < 0
  ) {
    return null;
  }
  if (message.ok === true) {
    if (
      typeof message.svg !== "string" ||
      typeof message.width !== "number" ||
      typeof message.height !== "number" ||
      !Number.isFinite(message.width) ||
      !Number.isFinite(message.height) ||
      message.width <= 0 ||
      message.height <= 0 ||
      byteLength(message.svg) > MERMAID_MAX_SVG_BYTES
    ) {
      return null;
    }
    const svg = sanitizeMermaidSvg(message.svg);
    if (!svg.includes("<svg")) {
      return null;
    }
    return {
      v: MERMAID_MESSAGE_VERSION,
      type: "result",
      requestId: message.requestId,
      generation: message.generation,
      ok: true,
      svg,
      width: message.width,
      height: message.height,
    };
  }
  if (message.ok !== false || typeof message.error !== "string") {
    return null;
  }
  return {
    v: MERMAID_MESSAGE_VERSION,
    type: "result",
    requestId: message.requestId,
    generation: message.generation,
    ok: false,
    error: message.error.slice(0, 180),
  };
}

export function serializeMermaidRenderRequest(request: MermaidRenderRequest) {
  return JSON.stringify(request);
}

export function isAllowedMermaidEngineUrl(url: string) {
  return (
    url === "about:blank" ||
    url === "about:srcdoc" ||
    url.startsWith("https://zen.local/mermaid")
  );
}
