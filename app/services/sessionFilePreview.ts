export type SessionFileKind =
  | "markdown"
  | "text"
  | "image"
  | "pdf"
  | "unsupported";

export type SessionFileRenderer =
  | "markdown"
  | "text"
  | "image"
  | "pdf"
  | "unsupported";

export interface SessionFileIdentity {
  agentId: string;
  processId: number;
  startedAt: number;
}

export interface SessionFileRequest extends SessionFileIdentity {
  path: string;
}

export interface SessionFileBinaryRequest extends SessionFileRequest {
  generation: string;
}

export function bindSessionFileRequestToGeneration(
  request: SessionFileRequest,
  metadata: SessionFileMetadata,
): SessionFileBinaryRequest {
  return {
    ...request,
    path: metadata.path,
    generation: metadata.generation,
  };
}

export interface SessionFileMetadata {
  name: string;
  path: string;
  relativePath: string;
  kind: SessionFileKind;
  contentType: string;
  size: number;
  modifiedAt: string;
  generation: string;
  tooLarge: boolean;
  previewLimitBytes: number;
}

export interface SessionFileTextPreview {
  content: string;
  bytesRead: number;
  truncated: boolean;
  generation: string;
}

export interface SessionFileBinarySource {
  uri: string;
  headers: Record<string, string>;
}

interface SessionFileCapabilityResponse {
  version?: unknown;
  device_id?: unknown;
  expires_at_ms?: unknown;
  get_signature?: unknown;
  head_signature?: unknown;
}

export interface SessionFileReadCapability {
  deviceId: string;
  expiresAtMS: number;
  getSignature: string;
  headSignature: string;
}

export interface SessionFilePreviewState {
  reference: string | null;
  status: "closed" | "loading" | "ready" | "error";
  metadata: SessionFileMetadata | null;
  text: SessionFileTextPreview | null;
  binarySource: SessionFileBinarySource | null;
  error: string | null;
  stale: boolean;
  requestEpoch: number;
}

export type SessionFilePreviewAction =
  | { type: "open"; reference: string }
  | { type: "metadata_loaded"; metadata: SessionFileMetadata }
  | { type: "text_loaded"; text: SessionFileTextPreview }
  | { type: "binary_ready"; source: SessionFileBinarySource }
  | { type: "ready" }
  | { type: "failed"; message: string; stale: boolean }
  | { type: "retry" }
  | { type: "close" | "context_changed" };

const KNOWN_FILE_BASENAMES = new Set([
  ".env",
  ".gitignore",
  ".npmrc",
  "agents.md",
  "dockerfile",
  "go.mod",
  "go.sum",
  "license",
  "makefile",
  "readme",
  "readme.md",
]);

const FILE_EXTENSION_RE =
  /\.(?:c|cc|conf|cpp|css|csv|env|gif|go|graphql|h|hpp|html?|ini|java|jpe?g|js|json|jsx|kt|kts|log|lua|m|markdown|md|mdx|mm|pdf|php|plist|png|properties|py|rb|rs|sh|sql|swift|toml|ts|tsx|txt|webp|xml|ya?ml|zsh)$/i;

export const initialSessionFilePreviewState: SessionFilePreviewState = {
  reference: null,
  status: "closed",
  metadata: null,
  text: null,
  binarySource: null,
  error: null,
  stale: false,
  requestEpoch: 0,
};

export function recognizeSessionFileReference(
  rawValue: string | null | undefined,
): string | null {
  let value = rawValue?.trim() || "";
  if (!value) return null;
  if (value.startsWith("<") && value.endsWith(">")) {
    value = value.slice(1, -1).trim();
  }
  if (value.startsWith("file://")) {
    try {
      const parsed = new URL(value);
      if (parsed.protocol !== "file:" || parsed.host) return null;
      value = decodeURIComponent(parsed.pathname);
    } catch {
      return null;
    }
  } else if (
    !/^[a-z]:[\\/]/i.test(value) &&
    /^[a-z][a-z0-9+.-]*:/i.test(value)
  ) {
    return null;
  }
  if (value.startsWith("#")) return null;

  value = value.replace(/#L\d+(?:C\d+)?$/i, "");
  value = value.replace(/:(\d+)(?::\d+)?$/, "");
  try {
    value = decodeURIComponent(value);
  } catch {
    return null;
  }
  value = value.trim();
  if (!value || value.includes("\u0000") || value.length > 4096) return null;

  const normalizedForName = value.replace(/\\/g, "/");
  const basename = normalizedForName.split("/").at(-1)?.toLowerCase() || "";
  const pathLike =
    normalizedForName.startsWith("/") ||
    normalizedForName.startsWith("./") ||
    normalizedForName.startsWith("../") ||
    normalizedForName.includes("/");
  if (
    !pathLike &&
    !FILE_EXTENSION_RE.test(basename) &&
    !KNOWN_FILE_BASENAMES.has(basename)
  ) {
    return null;
  }
  return value;
}

export function classifySessionFileRenderer(
  kind: SessionFileKind,
): SessionFileRenderer {
  switch (kind) {
    case "markdown":
    case "text":
    case "image":
      return kind;
    case "pdf":
      return "pdf";
    default:
      return "unsupported";
  }
}

export function reduceSessionFilePreviewState(
  state: SessionFilePreviewState,
  action: SessionFilePreviewAction,
): SessionFilePreviewState {
  switch (action.type) {
    case "open":
      return {
        ...initialSessionFilePreviewState,
        reference: action.reference,
        status: "loading",
        requestEpoch: state.requestEpoch + 1,
      };
    case "metadata_loaded":
      return {
        ...state,
        status: "loading",
        metadata: action.metadata,
        text: null,
        binarySource: null,
        error: null,
        stale: false,
      };
    case "text_loaded":
      return {
        ...state,
        status: "ready",
        text: action.text,
        binarySource: null,
        error: null,
        stale: false,
      };
    case "binary_ready":
      return {
        ...state,
        status: "ready",
        text: null,
        binarySource: action.source,
        error: null,
        stale: false,
      };
    case "ready":
      return { ...state, status: "ready", error: null, stale: false };
    case "failed":
      return {
        ...state,
        status: "error",
        text: null,
        binarySource: null,
        error: action.message,
        stale: action.stale,
      };
    case "retry":
      if (!state.reference) return state;
      return {
        ...state,
        status: "loading",
        metadata: null,
        text: null,
        binarySource: null,
        error: null,
        stale: false,
        requestEpoch: state.requestEpoch + 1,
      };
    case "close":
    case "context_changed":
      return initialSessionFilePreviewState;
    default:
      return state;
  }
}

export function sessionFilePreviewScopeKey(input: {
  serverId: string;
  serverUrl: string;
  daemonId: string;
  agentId: string;
  processId?: number;
  startedAt?: number;
  cwd?: string;
}): string {
  return [
    input.serverId,
    input.serverUrl,
    input.daemonId,
    input.agentId,
    input.processId ?? "",
    input.startedAt ?? "",
    input.cwd?.trim() || "",
  ].join("\u0000");
}

export function buildSessionFileBinaryUrl(
  serverUrl: string,
  request: SessionFileBinaryRequest,
): string | null {
  try {
    const url = new URL(serverUrl);
    if (url.protocol === "ws:") url.protocol = "http:";
    if (url.protocol === "wss:") url.protocol = "https:";
    if (url.protocol !== "http:" && url.protocol !== "https:") return null;
    url.pathname = "/session-file";
    url.search = "";
    url.hash = "";
    url.searchParams.set("agent_id", request.agentId);
    url.searchParams.set("process_id", String(request.processId));
    url.searchParams.set("started_at", String(request.startedAt));
    url.searchParams.set("path", request.path);
    url.searchParams.set("generation", request.generation);
    return url.toString();
  } catch {
    return null;
  }
}

export async function buildSessionFileBinarySource(
  serverId: string,
  daemonId: string,
  request: SessionFileBinaryRequest,
): Promise<SessionFileBinarySource> {
  const { resolveCanonicalServerURL } = await import("./pinnedTransport");
  const { getServerById } = await import("./storage");
  const server = await getServerById(serverId);
  if (!server || server.daemonId !== daemonId) {
    throw new Error(
      "The current server connection changed. Reopen the Session file and try again.",
    );
  }
  const transportURL = await resolveCanonicalServerURL(server);
  const uri = buildSessionFileBinaryUrl(transportURL, request);
  if (!uri) {
    throw new Error("Server file stream URL is unavailable.");
  }
  const capabilityURL = buildSessionFileCapabilityUrl(uri);
  if (!capabilityURL) {
    throw new Error("Server file authorization URL is unavailable.");
  }
  const { buildAuthorizationHeader } = await import("./auth");
  const authorizationHeader = await buildAuthorizationHeader({
    daemonId,
    purpose: "zen-session-file",
  });
  const response = await fetch(capabilityURL, {
    method: "POST",
    headers: {
      Authorization: authorizationHeader,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      agent_id: request.agentId,
      process_id: request.processId,
      started_at: request.startedAt,
      path: request.path,
      generation: request.generation,
    }),
  });
  if (!response.ok) {
    const error = new Error(
      sessionFileCapabilityError(response.status),
    ) as Error & {
      code?: string;
    };
    if (response.status === 409) {
      error.code = "session_file_changed";
    }
    throw error;
  }
  let capabilityPayload: SessionFileCapabilityResponse;
  try {
    capabilityPayload =
      (await response.json()) as SessionFileCapabilityResponse;
  } catch {
    throw new Error(
      "The daemon returned an unreadable Session file authorization. Update zen, then refresh.",
    );
  }
  const capability = normalizeSessionFileCapability(capabilityPayload);
  return {
    uri: appendSessionFileCapabilityQuery(uri, capability),
    headers: {
      "Cache-Control": "no-store",
    },
  };
}

function buildSessionFileCapabilityUrl(uri: string): string | null {
  try {
    const url = new URL(uri);
    url.pathname = "/session-file-capability";
    url.search = "";
    url.hash = "";
    return url.toString();
  } catch {
    return null;
  }
}

function normalizeSessionFileCapability(
  value: SessionFileCapabilityResponse,
): SessionFileReadCapability {
  const deviceId =
    typeof value.device_id === "string" ? value.device_id.trim() : "";
  const expiresAtMS =
    typeof value.expires_at_ms === "number" &&
    Number.isSafeInteger(value.expires_at_ms)
      ? value.expires_at_ms
      : 0;
  const getSignature =
    typeof value.get_signature === "string"
      ? value.get_signature.toLowerCase()
      : "";
  const headSignature =
    typeof value.head_signature === "string"
      ? value.head_signature.toLowerCase()
      : "";
  if (
    value.version !== 1 ||
    !deviceId ||
    expiresAtMS <= Date.now() ||
    !/^[0-9a-f]{128}$/.test(getSignature) ||
    !/^[0-9a-f]{128}$/.test(headSignature)
  ) {
    throw new Error(
      "The daemon returned an invalid Session file authorization. Refresh and try again.",
    );
  }
  return { deviceId, expiresAtMS, getSignature, headSignature };
}

export function appendSessionFileCapabilityQuery(
  uri: string,
  capability: SessionFileReadCapability,
): string {
  const url = new URL(uri);
  url.searchParams.delete("auth");
  url.searchParams.set("file_cap_device", capability.deviceId);
  url.searchParams.set("file_cap_expires", String(capability.expiresAtMS));
  url.searchParams.set("file_cap_get", capability.getSignature);
  url.searchParams.set("file_cap_head", capability.headSignature);
  return url.toString();
}

function sessionFileCapabilityError(status: number): string {
  switch (status) {
    case 401:
      return "Session file authorization was rejected. Refresh the preview to sign a new request.";
    case 404:
      return "This zen daemon does not support retry-safe Session file previews. Update zen, then refresh.";
    case 409:
      return "The Session or file changed before the preview could be authorized. Refresh and try again.";
    case 413:
      return "This file exceeds the daemon's Session preview limit.";
    default:
      return `Could not authorize the Session file preview (HTTP ${status}). Refresh and try again.`;
  }
}

export function normalizeSessionFileMetadata(
  value: unknown,
): SessionFileMetadata {
  const source =
    value && typeof value === "object"
      ? (value as Record<string, unknown>)
      : {};
  const kind = normalizeSessionFileKind(source.kind);
  const metadata: SessionFileMetadata = {
    name: boundedString(source.name, 1024),
    path: boundedString(source.path, 4096),
    relativePath: boundedString(source.relative_path, 4096),
    kind,
    contentType: boundedString(source.content_type, 256),
    size: finiteNonnegativeNumber(source.size),
    modifiedAt: boundedString(source.modified_at, 128),
    generation: boundedString(source.generation, 256),
    tooLarge: source.too_large === true,
    previewLimitBytes: finiteNonnegativeNumber(source.preview_limit_bytes),
  };
  if (!metadata.name || !metadata.path || !metadata.generation) {
    throw new Error("Server returned incomplete file metadata.");
  }
  return metadata;
}

export function normalizeSessionFileText(
  value: unknown,
): SessionFileTextPreview {
  const source =
    value && typeof value === "object"
      ? (value as Record<string, unknown>)
      : {};
  const content = typeof source.content === "string" ? source.content : "";
  if (content.length > 512 * 1024) {
    throw new Error("Server returned an oversized text preview.");
  }
  return {
    content,
    bytesRead: finiteNonnegativeNumber(source.bytes_read),
    truncated: source.truncated === true,
    generation: boundedString(source.generation, 256),
  };
}

export function isStaleSessionFileError(error: unknown): boolean {
  const code =
    error && typeof error === "object" && "code" in error
      ? String((error as { code?: unknown }).code || "")
      : "";
  return (
    code === "session_file_changed" || code === "session_file_stale_session"
  );
}

export function formatSessionFileSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "—";
  if (bytes < 1024) return `${Math.round(bytes)} B`;
  const units = ["KB", "MB", "GB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const digits = value >= 10 ? 0 : 1;
  return `${value.toFixed(digits)} ${units[unit]}`;
}

export function sessionFileTooLargeMessage(
  metadata: SessionFileMetadata,
): string {
  const limit = metadata.previewLimitBytes
    ? formatSessionFileSize(metadata.previewLimitBytes)
    : "the V1 size limit";
  return `This ${formatSessionFileSize(metadata.size)} file exceeds Zen's ${limit} preview limit. It was not downloaded.`;
}

function normalizeSessionFileKind(value: unknown): SessionFileKind {
  switch (value) {
    case "markdown":
    case "text":
    case "image":
    case "pdf":
      return value;
    default:
      return "unsupported";
  }
}

function boundedString(value: unknown, max: number): string {
  return typeof value === "string" ? value.slice(0, max) : "";
}

function finiteNonnegativeNumber(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0
    ? value
    : 0;
}
