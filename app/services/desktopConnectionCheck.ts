import type { DaemonAssertionInput } from "./auth";
import { normalizeFixedHex, verifyDesktopCapabilitySignature } from "./protocolCrypto";
import type { StoredServer } from "./storedServerContract";

export class DesktopConnectionUnavailable extends Error {
  constructor() { super("The desktop server is temporarily unavailable."); }
}

export class DesktopPreflightError extends Error {
  constructor(readonly code: string, message: string, readonly recovery = "") {
    super(message);
    this.name = "DesktopPreflightError";
  }
}

export interface DesktopProofDependencies {
  fetch: (url: string, init: Pick<RequestInit, "headers" | "signal" | "redirect">) => Promise<Pick<Response, "ok" | "status" | "url" | "redirected" | "body">>;
  authorization: (purpose: "zen-probe" | "zen-desktop-capability") => Promise<string>;
  verify: (input: DaemonAssertionInput) => boolean;
  timeoutMs?: number;
}

export interface DesktopCapability {
  deviceTrust: string;
  scopeVersion: number;
  requestEncrypted: boolean;
  identityTls: boolean;
  identityServerName: string;
  transportPin: string;
  hostStatus: string;
  hostBroker: boolean;
  currentSession: boolean;
  lockLogin: boolean;
  surface: string;
  session: string;
  unattended: boolean;
  reason: string;
  recovery: string;
}

const PREFLIGHT_MESSAGES: Record<string, string> = {
  desktop_tls_required: "Unattended desktop needs this computer's identity-bound encrypted transport. The unencrypted LAN switch is only for attended assistance.",
  desktop_scope_required: "This phone has terminal access only. Run zen pair on the computer and scan once to grant unattended desktop.",
  host_setup_required: "No current desktop session for this zen process. Start zen from the logged-in session, or run one OS-admin zen desktop-host --install for lock and login after reboot.",
};

export async function verifyDesktopServer(server: Pick<StoredServer, "daemonId" | "daemonPublicKey">, desktop: string,
  dependencies: DesktopProofDependencies, signal?: AbortSignal): Promise<void> {
  const endpoint = httpEndpoint(desktop);
  for (const purpose of ["zen-health", "zen-probe"] as const) {
    const stage = purpose === "zen-health" ? "health" : "auth-check";
    endpoint.pathname = stage === "health" ? "/health" : "/auth-check";
    const payload = await fetchSignedJSON(endpoint.toString(), stage, purpose === "zen-probe" ? "zen-probe" : undefined, server, dependencies, signal);
    if (purpose === "zen-probe" && payload.ok !== true) {
      throw new Error("The endpoint did not prove the identity of this paired computer (auth-check:ok).");
    }
  }
}

export async function fetchDesktopCapability(server: Pick<StoredServer, "daemonId" | "daemonPublicKey">, desktop: string,
  dependencies: DesktopProofDependencies, signal?: AbortSignal): Promise<DesktopCapability> {
  const endpoint = httpEndpoint(desktop);
  endpoint.pathname = "/desktop/capability";
  const payload = await fetchSignedJSON(endpoint.toString(), "desktop-capability", "zen-desktop-capability", server, dependencies, signal);
  const transport = asRecord(payload.transport);
  const host = asRecord(payload.host);
  const connect = asRecord(payload.connect);
  const pin = typeof transport.transport_pin === "string" ? transport.transport_pin.toLowerCase() : "";
  const identityTls = transport.identity_tls === true;
  const binding = new TextEncoder().encode([
    server.daemonId.trim().toLowerCase(),
    server.daemonPublicKey.trim().toLowerCase(),
    identityTls ? pin : "",
    identityTls ? "true" : "false",
  ].join("\n"));
  if (!verifyDesktopCapabilitySignature({
    daemonPublicKey: server.daemonPublicKey,
    bindingPayload: binding,
    signatureHex: typeof payload.capability_signature === "string" ? payload.capability_signature : "",
  })) {
    throw new Error("The desktop capability proof did not match this paired computer.");
  }
  if (identityTls && !/^[0-9a-f]{64}$/.test(pin)) {
    throw new Error("The desktop identity pin is invalid.");
  }
  const reason = typeof connect.reason === "string" ? connect.reason : "";
  return {
    deviceTrust: typeof payload.device_trust === "string" ? payload.device_trust : "",
    scopeVersion: Number(payload.desktop_scope_version) || 0,
    requestEncrypted: transport.request_encrypted === true,
    identityTls,
    identityServerName: typeof transport.identity_server_name === "string" ? transport.identity_server_name : "",
    transportPin: identityTls ? pin : "",
    hostStatus: typeof host.status === "string" ? host.status : "",
    hostBroker: host.broker === true,
    currentSession: host.current_session === true,
    lockLogin: host.lock_login === true,
    surface: typeof host.surface === "string" ? host.surface : "",
    session: typeof host.session === "string" ? host.session : "",
    unattended: connect.unattended === true,
    reason,
    recovery: typeof connect.recovery === "string" ? connect.recovery : "",
  };
}

export function desktopPreflightError(capability: DesktopCapability): DesktopPreflightError | null {
  if (capability.unattended && (capability.requestEncrypted || capability.identityTls) && capability.scopeVersion === 1) {
    return null;
  }
  const code = capability.reason || (!capability.scopeVersion ? "desktop_scope_required" :
    !capability.identityTls && !capability.requestEncrypted ? "desktop_tls_required" : "host_setup_required");
  return new DesktopPreflightError(code, PREFLIGHT_MESSAGES[code] || "Desktop is not ready.", capability.recovery);
}

function httpEndpoint(desktop: string): URL {
  const endpoint = new URL(desktop);
  endpoint.protocol = endpoint.protocol === "wss:" ? "https:" : "http:";
  return endpoint;
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" ? value as Record<string, unknown> : {};
}

async function fetchSignedJSON(url: string, stage: "health" | "auth-check" | "desktop-capability",
  authorizationPurpose: "zen-probe" | "zen-desktop-capability" | undefined,
  server: Pick<StoredServer, "daemonId" | "daemonPublicKey">, dependencies: DesktopProofDependencies, signal?: AbortSignal): Promise<Record<string, unknown>> {
  const controller = new AbortController();
  const abort = () => controller.abort();
  signal?.addEventListener("abort", abort);
  const timer = setTimeout(abort, dependencies.timeoutMs ?? 5000);
  try {
    if (signal?.aborted) throw new Error("Desktop connection cancelled.");
    const headers = authorizationPurpose ? { Authorization: await dependencies.authorization(authorizationPurpose) } : undefined;
    if (signal?.aborted) throw new Error("Desktop connection cancelled.");
    let response: Awaited<ReturnType<DesktopProofDependencies["fetch"]>>;
    try {
      response = await dependencies.fetch(url, { headers, signal: controller.signal, redirect: "error" });
    } catch {
      if (signal?.aborted) throw new Error("Desktop connection cancelled.");
      throw new DesktopConnectionUnavailable();
    }
    if (response.redirected || (response.url && response.url !== url)) throw new Error("Desktop endpoint redirects are not allowed.");
    if (response.status >= 500) throw new DesktopConnectionUnavailable();
    if (!response.ok) {
      throw response.status === 401
        ? new DesktopPreflightError("device_revoked", "This device is no longer paired with the computer.")
        : new Error("The desktop server did not pass its connection check.");
    }
    if (!response.body) throw new Error("The desktop server returned no identity proof.");
    const payload = JSON.parse(new TextDecoder().decode(await readBounded(response.body, controller)));
    if (!payload || typeof payload !== "object") throw new Error("The desktop server returned an invalid identity proof.");
    const timestamp = Date.parse(payload.assertion_timestamp);
    const purpose = authorizationPurpose ?? "zen-health";
    const servedDaemonId = normalizeFixedHex(typeof payload.daemon_id === "string" ? payload.daemon_id : "", 64);
    const servedPublicKey = normalizeFixedHex(typeof payload.daemon_public_key === "string" ? payload.daemon_public_key : "", 64);
    const expectedDaemonId = normalizeFixedHex(server.daemonId, 64);
    const expectedPublicKey = normalizeFixedHex(server.daemonPublicKey, 64);
    const signatureValid = dependencies.verify({ purpose, daemonId: server.daemonId, daemonPublicKey: server.daemonPublicKey,
      timestamp: payload.assertion_timestamp, nonceHex: payload.assertion_nonce, signatureHex: payload.assertion_signature });
    const failure = !expectedDaemonId || expectedDaemonId !== servedDaemonId ? "daemon_id" :
      !expectedPublicKey || expectedPublicKey !== servedPublicKey ? "daemon_public_key" :
      !Number.isFinite(timestamp) || Math.abs(Date.now() - timestamp) > 300000 ? "assertion_timestamp" :
      !signatureValid ? "assertion_signature" : "";
    if (failure) {
      throw new Error(`The endpoint did not prove the identity of this paired computer (${stage}:${failure}).`);
    }
    return payload as Record<string, unknown>;
  } finally { clearTimeout(timer); signal?.removeEventListener("abort", abort); }
}

async function readBounded(body: Pick<ReadableStream<Uint8Array>, "getReader">, controller: AbortController): Promise<Uint8Array> {
  const reader = body.getReader();
  const chunks: Uint8Array[] = []; let size = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > 16384) { controller.abort(); throw new Error("The desktop identity response is too large."); }
      chunks.push(value);
    }
  } finally { reader.releaseLock(); }
  const bytes = new Uint8Array(size); let offset = 0;
  for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
  return bytes;
}
