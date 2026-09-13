import { buildAuthorizationHeader, verifyDaemonAssertion } from "./auth";
import { resolveStoredServerURL } from "./pinnedTransport";
import type { StoredServer } from "./storage";
import { desktopLanOrigin, desktopPinnedIdentityPlan, desktopTransportPlan } from "./desktopTransportPolicy";
import { fetch } from "expo/fetch";
import {
  desktopPreflightError,
  fetchDesktopCapability,
  httpEndpoint,
  type DesktopProofDependencies,
  type MoonlightHostBootstrap,
  verifyDesktopServer,
} from "./desktopConnectionCheck";
import { MoonlightEnrollment } from "../modules/zen-remote-desktop/src";

interface PrepareOptions {
  /**
   * Bound enrollment receipt from a previous verified enrollment. It includes
   * the daemon, host key, native identity key and certificate fingerprint, so a
   * changed host/device/certificate never reuses a generic "enrolled" flag.
   */
  moonlightEnrolled?: string;
}

// expo-crypto exposes the platform CSPRNG; do not assume a browser global.
import { getRandomBytes } from "expo-crypto";

function randomHex(bytes: number): string {
  return Array.from(getRandomBytes(bytes), (b) => b.toString(16).padStart(2, "0")).join("");
}

/** Bounded abortable text read: stops accepting bytes past the limit. */
async function readBoundedText(body: ReadableStream<Uint8Array>, limit: number): Promise<string> {
  const reader = body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      if (!value) continue;
      size += value.byteLength;
      if (size > limit) {
        await reader.cancel().catch(() => undefined);
        throw new Error("enrollment_response_too_large");
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock?.();
  }
  const merged = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) { merged.set(chunk, offset); offset += chunk.byteLength; }
  return new TextDecoder().decode(merged);
}

export function moonlightReceiptKey(server: Pick<StoredServer, "daemonId">,
  moonlight: Pick<MoonlightHostBootstrap, "hostKey" | "identityKey">, fingerprint: string): string {
  return [server.daemonId, moonlight.hostKey, moonlight.identityKey, fingerprint].join(":");
}

/** Freshly authorized, redirect-rejecting control POST with a bounded body. */
async function postControl(server: StoredServer, resolved: string, proof: DesktopProofDependencies,
  path: string, body: Record<string, unknown>, signal?: AbortSignal): Promise<Record<string, unknown>> {
  const endpoint = httpEndpoint(resolved);
  endpoint.pathname = path;
  const controller = new AbortController();
  const abort = () => controller.abort();
  signal?.addEventListener("abort", abort);
  const timer = setTimeout(abort, proof.timeoutMs ?? 5000);
  try {
    const authorization = await proof.authorization("zen-desktop-capability");
    if (signal?.aborted) throw new Error("Desktop connection cancelled.");
    const response = await proof.fetch(endpoint.toString(), {
      method: "POST",
      headers: { Authorization: authorization, "Content-Type": "application/json" },
      body: JSON.stringify(body),
      signal: controller.signal,
      redirect: "error",
    });
    if (response.redirected || (response.url && response.url !== endpoint.toString())) {
      throw new Error("Desktop endpoint redirects are not allowed.");
    }
    // Read bounded text BEFORE accepting bytes past the limit: the daemon can
    // also answer with a plain-text error (for example a 409 pending body), so
    // a JSON parse crash is never the outcome.
    const raw = response.body ? await readBoundedText(response.body, 8192) : "";
    let payload: Record<string, unknown> = {};
    try {
      payload = raw ? JSON.parse(raw) : {};
    } catch {
      payload = { reason: raw.trim() };
    }
    if (!response.ok) {
      const reason = typeof payload.reason === "string" && payload.reason ? payload.reason : `http_${response.status}`;
      const error = new Error(reason);
      (error as Error & { status?: number }).status = response.status;
      throw error;
    }
    // Signed receipt policy: the response must carry a valid daemon assertion
    // for the expected daemon, and the enrollment result must target this
    // device/host. An unsigned or foreign receipt is never accepted.
    const servedDaemon = typeof payload.daemon_id === "string" ? payload.daemon_id.toLowerCase() : "";
    if (servedDaemon !== server.daemonId.trim().toLowerCase()) {
      throw new Error("enrollment_receipt_daemon_mismatch");
    }
    const timestamp = typeof payload.assertion_timestamp === "string" ? payload.assertion_timestamp : "";
    const nonce = typeof payload.assertion_nonce === "string" ? payload.assertion_nonce : "";
    const assertion = typeof payload.assertion_signature === "string" ? payload.assertion_signature : "";
    if (!timestamp || !nonce || !assertion ||
        !proof.verify({
          purpose: "zen-desktop-capability",
          daemonId: server.daemonId,
          daemonPublicKey: server.daemonPublicKey,
          timestamp,
          nonceHex: nonce,
          signatureHex: assertion,
        })) {
      throw new Error("enrollment_receipt_unverified");
    }
    return payload;
  } finally {
    clearTimeout(timer);
    signal?.removeEventListener("abort", abort);
  }
}

/**
 * Production enrollment caller: native identity -> fresh authorized begin ->
 * native detached signature -> authenticated complete. Pending means pairing
 * has not reached the host state yet; the caller retries after pairing.
 * 401/403/rejected/expired attempts are surfaced and never treated as enrolled.
 */
export interface MoonlightEnrollmentHandle {
  deviceID: string;
  attempt: string;
  nonce: string;
  signature: string;
  certPem: string;
  fingerprint: string;
  receipt: string;
}

/** Bound, retryable enrollment attempt for one device/host/certificate. */
export async function beginMoonlightEnrollment(server: StoredServer, moonlight: MoonlightHostBootstrap,
  signal?: AbortSignal): Promise<MoonlightEnrollmentHandle> {
  if (!MoonlightEnrollment) throw new Error("This build has no Moonlight enrollment support.");
  const resolved = await resolveStoredServerURL(server);
  const proof: DesktopProofDependencies = {
    fetch,
    authorization: (purpose) => buildAuthorizationHeader({ daemonId: server.daemonId, purpose }),
    verify: verifyDaemonAssertion,
  };
  const identity = await MoonlightEnrollment.moonlightEnrollmentIdentity(moonlight.identityKey);
  if (!identity.certPem || !identity.fingerprint) throw new Error("moonlight_identity_unavailable");
  if (signal?.aborted) throw new Error("Desktop connection cancelled.");
  const attempt = randomHex(16);
  const begin = await postControl(server, resolved, proof, "/desktop/moonlight/enroll/begin", { attempt }, signal);
  const nonce = typeof begin.nonce === "string" ? begin.nonce : "";
  if (!nonce) throw new Error("enrollment_challenge_missing");
  // The daemon's authenticated device must be the identity this app is using.
  if (begin.device_id !== moonlight.identityKey) throw new Error("enrollment_device_mismatch");
  if (signal?.aborted) throw new Error("Desktop connection cancelled.");
  const signature = await MoonlightEnrollment.moonlightSignEnrollment(moonlight.identityKey, attempt, nonce);
  if (signal?.aborted) throw new Error("Desktop connection cancelled.");
  return {
    deviceID: moonlight.identityKey,
    attempt, nonce, signature, certPem: identity.certPem, fingerprint: identity.fingerprint,
    receipt: moonlightReceiptKey(server, moonlight, identity.fingerprint),
  };
}

/** Finalizes the same attempt; pending keeps the handle for a later retry. */
export async function completeMoonlightEnrollment(server: StoredServer, handle: MoonlightEnrollmentHandle,
  signal?: AbortSignal): Promise<"verified" | "pending"> {
  const resolved = await resolveStoredServerURL(server);
  const proof: DesktopProofDependencies = {
    fetch,
    authorization: (purpose) => buildAuthorizationHeader({ daemonId: server.daemonId, purpose }),
    verify: verifyDaemonAssertion,
  };
  if (signal?.aborted) throw new Error("Desktop connection cancelled.");
  try {
    const complete = await postControl(server, resolved, proof, "/desktop/moonlight/enroll/complete", {
      attempt: handle.attempt, nonce: handle.nonce, client_cert_pem: handle.certPem, signature: handle.signature,
    }, signal);
    if (complete.enrolled !== true) return "pending";
    // Bind the receipt to this exact attempt/certificate/host: a generic
    // assertion or a different device's success is not enough.
    if (complete.attempt !== handle.attempt || complete.fingerprint !== handle.fingerprint) {
      throw new Error("enrollment_receipt_mismatch");
    }
    if (complete.device_id !== handle.deviceID) throw new Error("enrollment_device_mismatch");
    if (typeof complete.host_key === "string" && complete.host_key) {
      const expected = handle.receipt.split(":")[1] ?? "";
      if (expected && complete.host_key !== expected) throw new Error("enrollment_receipt_host_mismatch");
    }
    return "verified";
  } catch (error) {
    if ((error as Error).message === "enrollment_pending") return "pending";
    throw error;
  }
}

export async function enrollMoonlightConnection(server: StoredServer, moonlight: MoonlightHostBootstrap,
  signal?: AbortSignal): Promise<{ state: "verified" | "pending"; handle: MoonlightEnrollmentHandle }> {
  const handle = await beginMoonlightEnrollment(server, moonlight, signal);
  const state = await completeMoonlightEnrollment(server, handle, signal);
  return { state, handle };
}

export async function prepareDesktopConnection(server: StoredServer, inputGeneration: string, signal?: AbortSignal, options: PrepareOptions = {}): Promise<string> {
  const resolved = await resolveStoredServerURL(server);
  const proof: DesktopProofDependencies = {
    fetch,
    authorization: (purpose) => buildAuthorizationHeader({ daemonId: server.daemonId, purpose }),
    verify: verifyDaemonAssertion,
  };
  await verifyDesktopServer(server, resolved, proof, signal);
  const capability = await fetchDesktopCapability(server, resolved, proof, signal);
  const blocking = desktopPreflightError(capability);
  // A server-verified trusted deployment uses the direct trusted-LAN plan even
  // when Zen also offers optional identity TLS; the extra pinned tunnel is only
  // for deployments that actually require it.
  const identityLan = !capability.trustedIngress && capability.identityTls && !!desktopLanOrigin(server)
    && server.transportKind !== "link" && capability.scopeVersion === 1 && capability.unattended;
  const authorization = await buildAuthorizationHeader({ daemonId: server.daemonId, purpose: "zen-desktop" });
  // The control channel may go through a local pinned tunnel while the native
  // engine needs its own directly reachable computer IP. Prefer Moonlight when
  // the engine endpoint is direct, before selecting the pinned-link control plan.
  const lanOrigin = desktopLanOrigin(server);
  const engineHost = lanOrigin ? new URL(lanOrigin).hostname : new URL(server.url).hostname;
  const engineDirect = server.transportKind !== "link" &&
    !/^(127\.|localhost$|\[?::1)/i.test(engineHost);
  if (capability.moonlight?.available && capability.scopeVersion > 0 && engineDirect) {
    if (signal?.aborted) throw new Error("Desktop connection cancelled.");
    return JSON.stringify({
      transport: "moonlight",
      authorization,
      inputGeneration,
      moonlight: {
        ...capability.moonlight,
        host: engineHost,
        pairOnly: capability.moonlight.admission !== "verified",
      },
      moonlightReceipt: options.moonlightEnrolled ?? "",
    });
  }
  if (blocking && !identityLan) throw blocking;
  if (signal?.aborted) throw new Error("Desktop connection cancelled.");
  let plan;
  if (identityLan) {
    const source = new URL(server.url);
    const port = Number(source.port) || 9876;
    const { startPinnedTunnel } = await import("../modules/zen-link-transport/src");
    const tunnel = await startPinnedTunnel(`desktop:${server.id}`, source.hostname, port, capability.transportPin, "on-demand");
    plan = desktopPinnedIdentityPlan(`ws://127.0.0.1:${tunnel.port}`, `wss://${source.hostname}:${port}`, capability.transportPin);
  } else {
    plan = desktopTransportPlan(server, resolved, { trustedIngress: capability.trustedIngress });
    if (plan.transport === "trusted-lan" && !capability.trustedIngress) {
      // Only an untrusted deployment needs the legacy encrypted transport.
      throw new Error("Unattended desktop cannot use unencrypted LAN transport.");
    }
  }
  return JSON.stringify({ ...plan, authorization, inputGeneration, mode: "unattended" });
}
