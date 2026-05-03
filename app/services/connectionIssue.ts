import {
  buildAuthorizationHeader,
  normalizeDaemonId,
  normalizePublicKeyHex,
  verifyDaemonAssertion,
} from "./auth";
import { Colors, type AppColors } from "../constants/tokens";

export type ConnectionIssueCode =
  | "invalid_url"
  | "network_unreachable"
  | "device_not_paired"
  | "wrong_daemon"
  | "path_not_found"
  | "proxy_error"
  | "websocket_upgrade_failed"
  | "unexpected_http_response";

export interface ConnectionIssue {
  code: ConnectionIssueCode;
  title: string;
  detail: string;
  hint: string;
  checkedAt: number;
  httpStatus?: number;
}

interface TrustedDaemonPayload {
  daemon_id?: string;
  daemon_public_key?: string;
  assertion_timestamp?: string;
  assertion_nonce?: string;
  assertion_signature?: string;
}

const PROBE_TIMEOUT_MS = 4000;

export async function diagnoseConnectionIssue(input: {
  serverUrl: string;
  daemonId: string;
  daemonPublicKey: string;
}): Promise<ConnectionIssue> {
  const daemonId = normalizeDaemonId(input.daemonId);
  const daemonPublicKey = normalizePublicKeyHex(input.daemonPublicKey);
  const probeURL = buildProbeURL(input.serverUrl);
  const healthURL = buildPathURL(input.serverUrl, "/health");
  const authCheckURL = buildPathURL(input.serverUrl, "/auth-check");

  if (!probeURL || !healthURL || !authCheckURL) {
    return createIssue(
      "invalid_url",
      "Invalid endpoint URL",
      "The saved server URL could not be parsed.",
      "Use a full ws:// or wss:// endpoint that points at zen-daemon, usually ending in /ws.",
    );
  }
  if (!daemonId || !daemonPublicKey) {
    return createIssue(
      "wrong_daemon",
      "Missing daemon identity",
      "This saved server does not include a trusted daemon identity.",
      "Re-import the pairing link from zen-daemon so zen can bind this endpoint to a daemon key.",
    );
  }

  try {
    const healthResponse = await fetchWithTimeout(healthURL, { method: "GET" });
    if (!healthResponse.ok) {
      return mapProbeFailure(healthResponse.status, "health");
    }

    const healthPayload = await readJSON<TrustedDaemonPayload>(healthResponse);
    if (
      !isTrustedDaemonPayload(healthPayload, {
        purpose: "zen-health",
        daemonId,
        daemonPublicKey,
      })
    ) {
      return createIssue(
        "wrong_daemon",
        "Wrong daemon identity",
        "The endpoint is reachable, but it did not prove possession of the trusted daemon key for this server.",
        "Re-import the pairing link for the correct daemon, or update the endpoint if your tunnel now points somewhere else.",
        healthResponse.status,
      );
    }

    const authHeader = await buildAuthorizationHeader({
      daemonId,
      purpose: "zen-probe",
    });
    const signedHeaders = { Authorization: authHeader };

    const authCheckResponse = await fetchWithTimeout(authCheckURL, {
      method: "GET",
      headers: signedHeaders,
    });
    switch (authCheckResponse.status) {
      case 200:
        break;
      case 401:
        return createIssue(
          "device_not_paired",
          "Device is not paired",
          "The daemon is reachable and trusted, but it rejected this device identity.",
          "Import a fresh pairing link from zen-daemon on this machine to enroll this phone again.",
          authCheckResponse.status,
        );
      default:
        return mapProbeFailure(authCheckResponse.status, "auth-check");
    }

    const authCheckPayload =
      await readJSON<TrustedDaemonPayload>(authCheckResponse);
    if (
      !isTrustedDaemonPayload(authCheckPayload, {
        purpose: "zen-probe",
        daemonId,
        daemonPublicKey,
      })
    ) {
      return createIssue(
        "wrong_daemon",
        "Wrong daemon identity",
        "The endpoint answered the auth check, but its identity proof did not match the trusted daemon.",
        "Update the saved endpoint or re-import the pairing link from the daemon you actually want to trust.",
        authCheckResponse.status,
      );
    }

    const probeResponse = await fetchWithTimeout(probeURL, {
      method: "GET",
      headers: signedHeaders,
    });
    switch (probeResponse.status) {
      case 200:
        break;
      case 401:
        return createIssue(
          "device_not_paired",
          "Device is not paired",
          "The daemon reached the WebSocket endpoint, but it rejected this device identity at the /ws probe.",
          "Import a fresh pairing link from zen-daemon on this machine to enroll this phone again.",
          probeResponse.status,
        );
      default:
        return mapProbeFailure(probeResponse.status, "ws");
    }

    const probePayload = await readJSON<TrustedDaemonPayload>(probeResponse);
    if (
      !isTrustedDaemonPayload(probePayload, {
        purpose: "zen-probe",
        daemonId,
        daemonPublicKey,
      })
    ) {
      return createIssue(
        "wrong_daemon",
        "Wrong daemon identity",
        "The WebSocket endpoint responded, but it did not prove the trusted daemon identity for this server.",
        "Make sure the tunnel forwards to the correct zen-daemon instance rather than another service or machine.",
        probeResponse.status,
      );
    }

    return createIssue(
      "websocket_upgrade_failed",
      "WebSocket session failed after probes passed",
      "HTTP reachability, daemon identity, and device authorization all succeeded, but the live WebSocket session still failed to open.",
      "Check that your tunnel or reverse proxy forwards WebSocket Upgrade and Connection headers, and does not downgrade the connection to plain HTTP polling.",
      probeResponse.status,
    );
  } catch (error) {
    return buildNetworkIssue(error);
  }
}

export function connectionIssueAccent(
  issue: ConnectionIssue | null | undefined,
  colors: AppColors = Colors,
): string {
  if (!issue) return colors.disabledText;

  switch (issue.code) {
    case "device_not_paired":
      return colors.warning;
    case "wrong_daemon":
    case "network_unreachable":
    case "proxy_error":
    case "unexpected_http_response":
    case "invalid_url":
      return colors.dangerText;
    case "path_not_found":
    case "websocket_upgrade_failed":
      return colors.warning;
  }
}

function mapProbeFailure(
  status: number,
  endpointKind: "health" | "auth-check" | "ws",
): ConnectionIssue {
  switch (status) {
    case 404:
      return createIssue(
        "path_not_found",
        "Daemon routes are not exposed",
        `The endpoint returned HTTP 404 for zen-daemon's ${endpointKind} route.`,
        "Forward the full zen-daemon origin through your tunnel or reverse proxy, including /health, /auth-check, /pair, /upload, and /ws.",
        status,
      );
    case 400:
    case 405:
    case 426:
      return createIssue(
        "websocket_upgrade_failed",
        "WebSocket handshake failed",
        `The ${endpointKind} request reached the endpoint, but the tunnel or proxy did not behave like zen-daemon expects.`,
        "Check HTTPS/TLS and make sure your reverse proxy preserves WebSocket Upgrade and Connection headers all the way to zen-daemon.",
        status,
      );
    case 502:
    case 503:
    case 504:
      return createIssue(
        "proxy_error",
        "Proxy could not reach daemon",
        `The endpoint returned HTTP ${status}, which usually means your proxy or tunnel could not reach zen-daemon on localhost.`,
        "Check that zen-daemon is running on 127.0.0.1:9876 and that your tunnel points at the same local port.",
        status,
      );
    default:
      return createIssue(
        "unexpected_http_response",
        "Unexpected server response",
        `The endpoint returned HTTP ${status} instead of a valid zen-daemon response.`,
        "Make sure this URL points at zen-daemon rather than a website, API gateway, or some other service.",
        status,
      );
  }
}

function buildProbeURL(serverUrl: string): string | null {
  const trimmed = serverUrl.trim();
  if (!trimmed) return null;

  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol === "ws:") {
      parsed.protocol = "http:";
    } else if (parsed.protocol === "wss:") {
      parsed.protocol = "https:";
    } else if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return null;
    }

    parsed.search = "";
    parsed.hash = "";
    return parsed.toString();
  } catch {
    return null;
  }
}

function buildPathURL(serverUrl: string, pathname: string): string | null {
  const probeURL = buildProbeURL(serverUrl);
  if (!probeURL) return null;

  try {
    const parsed = new URL(probeURL);
    parsed.pathname = pathname;
    parsed.search = "";
    parsed.hash = "";
    return parsed.toString();
  } catch {
    return null;
  }
}

async function fetchWithTimeout(
  url: string,
  init: RequestInit,
  timeoutMs: number = PROBE_TIMEOUT_MS,
): Promise<Response> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);

  try {
    return await fetch(url, {
      ...init,
      signal: controller.signal,
    });
  } finally {
    clearTimeout(timeout);
  }
}

async function readJSON<T extends object>(
  response: Response,
): Promise<T | null> {
  try {
    return (await response.json()) as T;
  } catch {
    return null;
  }
}

function isTrustedDaemonPayload(
  payload: TrustedDaemonPayload | null,
  input: {
    purpose: string;
    daemonId: string;
    daemonPublicKey: string;
  },
): boolean {
  if (!payload) {
    return false;
  }

  const daemonId = normalizeDaemonId(readString(payload.daemon_id));
  const daemonPublicKey = normalizePublicKeyHex(
    readString(payload.daemon_public_key),
  );
  if (
    daemonId !== input.daemonId ||
    daemonPublicKey !== input.daemonPublicKey
  ) {
    return false;
  }

  return verifyDaemonAssertion({
    purpose: input.purpose,
    daemonId,
    daemonPublicKey,
    timestamp: readString(payload.assertion_timestamp),
    nonceHex: readString(payload.assertion_nonce),
    signatureHex: readString(payload.assertion_signature),
  });
}

function buildNetworkIssue(error: unknown): ConnectionIssue {
  const rawMessage = error instanceof Error ? error.message.trim() : "";
  const normalizedMessage = rawMessage.toLowerCase();

  if (error instanceof Error && error.name === "AbortError") {
    return createIssue(
      "network_unreachable",
      "Server timed out",
      "The daemon did not respond before the probe timed out.",
      "Check tunnel latency, firewall rules, and whether zen-daemon is sleeping or overloaded.",
    );
  }

  if (
    normalizedMessage.includes("enotfound") ||
    normalizedMessage.includes("name resolution") ||
    normalizedMessage.includes("dns") ||
    normalizedMessage.includes("host not found")
  ) {
    return createIssue(
      "network_unreachable",
      "DNS lookup failed",
      rawMessage ||
        "The hostname could not be resolved before zen reached the daemon.",
      "Check the hostname spelling, your DNS configuration, or try a different tunnel hostname.",
    );
  }

  if (
    normalizedMessage.includes("econnrefused") ||
    normalizedMessage.includes("connection refused")
  ) {
    return createIssue(
      "network_unreachable",
      "Daemon refused connection",
      "The host is reachable, but nothing accepted the connection on the configured port.",
      "Check that zen-daemon is running and that your tunnel or reverse proxy forwards to 127.0.0.1:9876.",
    );
  }

  if (
    normalizedMessage.includes("certificate") ||
    normalizedMessage.includes("ssl") ||
    normalizedMessage.includes("tls")
  ) {
    return createIssue(
      "network_unreachable",
      "TLS handshake failed",
      rawMessage ||
        "The HTTPS/TLS handshake failed before the WebSocket could connect.",
      "Check your certificate chain, hostname, and tunnel or reverse proxy TLS configuration.",
    );
  }

  return createIssue(
    "network_unreachable",
    "Server unreachable",
    rawMessage
      ? `The network request failed before a WebSocket connection could be established: ${rawMessage}`
      : "The network request failed before a WebSocket connection could be established.",
    "Check the hostname, DNS, tunnel, firewall, and whether zen-daemon is running.",
  );
}

function createIssue(
  code: ConnectionIssueCode,
  title: string,
  detail: string,
  hint: string,
  httpStatus?: number,
): ConnectionIssue {
  return {
    code,
    title,
    detail,
    hint,
    checkedAt: Date.now(),
    httpStatus,
  };
}

function readString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}
