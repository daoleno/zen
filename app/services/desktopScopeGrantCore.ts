import { desktopLanOrigin } from "./desktopTransportPolicy";
import type { StoredServer } from "./storage";
import type { DaemonAssertionInput } from "./auth";
import type { DesktopCapability } from "./desktopConnectionCheck";

// Canonical purpose shared with daemon/auth.DesktopGrantPurpose. The app signs
// this exact literal; the daemon authenticates and signs its confirmation with
// the same literal.
export const DESKTOP_GRANT_PURPOSE = "zen-device-admin:desktop-grant:POST:/desktop/scope";
export const DESKTOP_GRANT_VERSION = 1;

export interface DesktopGrantTunnelFactory {
  start(key: string, host: string, port: number, pin: string): Promise<{ port: number }>;
  stop(key: string): Promise<void>;
}

export interface DesktopGrantRequestDependencies {
  fetch: (url: string, init: { method: string; headers: Record<string, string>; body: string; signal?: AbortSignal; redirect: "error" }) =>
    Promise<Pick<Response, "ok" | "status" | "text" | "json">>;
  authorization: () => Promise<string>;
  identity: () => Promise<{ deviceId: string }>;
  verify: (input: DaemonAssertionInput) => boolean;
  timeoutMs?: number;
}

async function nativeTunnels(): Promise<DesktopGrantTunnelFactory> {
  const { startPinnedTunnel, stopPinnedTunnel } = await import("../modules/zen-link-transport/src");
  return {
    start: (key, host, port, pin) => startPinnedTunnel(key, host, port, pin, "on-demand"),
    stop: (key) => stopPinnedTunnel(key),
  };
}

function httpOrigin(value: string): string {
  const url = new URL(value);
  url.protocol = url.protocol === "wss:" ? "https:" : "http:";
  url.pathname = "";
  url.search = "";
  url.hash = "";
  return url.origin;
}

// A scope0 device must establish the same identity-bound encrypted transport
// that a scope1 connection would use before it can consent to scope1. The
// grant never requires desktop scope, so there is no circular dependency; the
// media admission path still does.
export async function desktopGrantTransport(
  server: StoredServer,
  resolved: string,
  capability: Pick<DesktopCapability, "identityTls" | "transportPin" | "trustedIngress">,
  tunnels?: DesktopGrantTunnelFactory,
): Promise<{ origin: string; release: () => Promise<void> }> {
  if (!capability.trustedIngress && (!capability.identityTls || !/^[0-9a-f]{64}$/i.test(capability.transportPin))) {
    throw new Error("Remote desktop requires this computer's identity-bound encrypted connection.");
  }
  if (server.transportKind === "link") {
    return { origin: httpOrigin(resolved), release: async () => {} };
  }
  const source = new URL(server.url);
  if (capability.trustedIngress) {
    // The daemon verified this request as arriving over the operator's trusted
    // deployment; no redundant local TLS tunnel and no certificate are needed.
    return { origin: httpOrigin(resolved), release: async () => {} };
  }
  if (desktopLanOrigin(server)) {
    const factory = tunnels ?? (await nativeTunnels());
    const key = `desktop-grant:${server.id}`;
    const tunnel = await factory.start(key, source.hostname, Number(source.port) || 9876, capability.transportPin);
    return { origin: `http://127.0.0.1:${tunnel.port}`, release: () => factory.stop(key).catch(() => undefined) };
  }
  if (source.protocol !== "wss:") {
    throw new Error("Remote desktop requires an encrypted connection to this computer.");
  }
  return { origin: httpOrigin(resolved), release: async () => {} };
}

export async function postDesktopGrant(
  origin: string,
  server: Pick<StoredServer, "daemonId" | "daemonPublicKey">,
  dependencies: DesktopGrantRequestDependencies,
  signal?: AbortSignal,
): Promise<void> {
  const controller = new AbortController();
  const abort = () => controller.abort();
  signal?.addEventListener("abort", abort);
  const timer = setTimeout(abort, dependencies.timeoutMs ?? 8000);
  try {
    if (signal?.aborted) throw new Error("Enable cancelled.");
    const authorization = await dependencies.authorization();
    if (signal?.aborted) throw new Error("Enable cancelled.");
    let response: Awaited<ReturnType<DesktopGrantRequestDependencies["fetch"]>>;
    try {
      response = await dependencies.fetch(`${origin.replace(/\/$/, "")}/desktop/scope`, {
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: authorization },
        body: JSON.stringify({ desktop_scope_version: DESKTOP_GRANT_VERSION }),
        signal: controller.signal,
        redirect: "error",
      });
    } catch {
      if (signal?.aborted) throw new Error("Enable cancelled.");
      throw new Error("Could not reach this computer to enable remote desktop.");
    }
    if (response.status >= 500) {
      throw new Error("The computer could not save desktop permission. Try again.");
    }
    if (!response.ok) {
      const detail = (await response.text()).trim();
      throw new Error(detail === "desktop_tls_required"
        ? "Remote desktop needs the encrypted connection to this computer."
        : "Could not enable remote desktop on this computer. Update Zen there and try again.");
    }
    const payload = await response.json() as Record<string, unknown>;
    const identity = await dependencies.identity();
    if (payload.ok !== true || payload.device_id !== identity.deviceId || Number(payload.desktop_scope_version) !== DESKTOP_GRANT_VERSION) {
      throw new Error("The computer did not confirm desktop permission for this phone.");
    }
    if (!dependencies.verify({
      purpose: DESKTOP_GRANT_PURPOSE,
      daemonId: server.daemonId,
      daemonPublicKey: server.daemonPublicKey,
      timestamp: typeof payload.assertion_timestamp === "string" ? payload.assertion_timestamp : null,
      nonceHex: typeof payload.assertion_nonce === "string" ? payload.assertion_nonce : null,
      signatureHex: typeof payload.assertion_signature === "string" ? payload.assertion_signature : null,
    })) {
      throw new Error("The desktop permission confirmation did not match this computer.");
    }
  } finally {
    clearTimeout(timer);
    signal?.removeEventListener("abort", abort);
  }
}
