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
  verifyDesktopServer,
} from "./desktopConnectionCheck";
import { MoonlightEnrollment } from "../modules/zen-remote-desktop/src";

interface PrepareOptions {
  /** Set after this device/identity/host completed enrollment successfully. */
  moonlightEnrolled?: boolean;
}

function randomHex(bytes: number): string {
  const value = new Uint8Array(bytes);
  crypto.getRandomValues(value);
  return Array.from(value, (b) => b.toString(16).padStart(2, "0")).join("");
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
    const payload = response.body ? JSON.parse(await new Response(response.body).text()) : {};
    if (!response.ok) {
      const reason = typeof payload?.reason === "string" ? payload.reason : `http_${response.status}`;
      const error = new Error(reason);
      (error as Error & { status?: number }).status = response.status;
      throw error;
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
export async function enrollMoonlightConnection(server: StoredServer, identityKey: string, signal?: AbortSignal): Promise<"verified" | "pending"> {
  if (!MoonlightEnrollment) throw new Error("This build has no Moonlight enrollment support.");
  const resolved = await resolveStoredServerURL(server);
  const proof: DesktopProofDependencies = {
    fetch,
    authorization: (purpose) => buildAuthorizationHeader({ daemonId: server.daemonId, purpose }),
    verify: verifyDaemonAssertion,
  };
  const attempt = randomHex(16);
  const identity = await MoonlightEnrollment.moonlightEnrollmentIdentity(identityKey);
  if (!identity.certPem || !identity.fingerprint) throw new Error("moonlight_identity_unavailable");
  if (signal?.aborted) throw new Error("Desktop connection cancelled.");
  const begin = await postControl(server, resolved, proof, "/desktop/moonlight/enroll/begin", { attempt }, signal);
  const nonce = typeof begin.nonce === "string" ? begin.nonce : "";
  if (!nonce) throw new Error("enrollment_challenge_missing");
  if (signal?.aborted) throw new Error("Desktop connection cancelled.");
  const signature = await MoonlightEnrollment.moonlightSignEnrollment(identityKey, attempt, nonce);
  if (signal?.aborted) throw new Error("Desktop connection cancelled.");
  try {
    const complete = await postControl(server, resolved, proof, "/desktop/moonlight/enroll/complete", {
      attempt, nonce, client_cert_pem: identity.certPem, signature,
    }, signal);
    return complete.enrolled === true ? "verified" : "pending";
  } catch (error) {
    if ((error as Error).message === "enrollment_pending") return "pending";
    throw error;
  }
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
  const identityLan = capability.identityTls && !!desktopLanOrigin(server) && server.transportKind !== "link"
    && capability.scopeVersion === 1 && capability.unattended;
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
        pairOnly: !options.moonlightEnrolled && capability.moonlight.admission !== "verified",
      },
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
    plan = desktopTransportPlan(server, resolved);
    if (plan.transport === "trusted-lan") {
      throw new Error("Unattended desktop cannot use unencrypted LAN transport.");
    }
  }
  return JSON.stringify({ ...plan, authorization, inputGeneration, mode: "unattended" });
}
