import { buildHTTPURL, buildSignedRequestHeaders } from "./pairing";
import { resolveCanonicalServerURL } from "./pinnedTransport";
import type { StoredServer } from "./storage";

const LATENCY_TIMEOUT_MS = 4000;

export interface ServerLatencySample {
  latencyMs: number;
  measuredAt: number;
}

export async function measureServerLatency(input: {
  server: StoredServer;
}): Promise<ServerLatencySample | null> {
  const transportURL = await resolveCanonicalServerURL(input.server);
  const authCheckURL = buildHTTPURL(transportURL, "/auth-check");
  if (!authCheckURL) {
    return null;
  }

  const headers = await buildSignedRequestHeaders({
    serverUrl: transportURL,
    daemonId: input.server.daemonId,
    purpose: "zen-probe",
  });

  const startedAt = monotonicNow();
  const response = await fetchWithTimeout(authCheckURL, {
    method: "GET",
    headers,
  });
  const latencyMs = Math.max(1, Math.round(monotonicNow() - startedAt));

  if (!response.ok) {
    return null;
  }

  return {
    latencyMs,
    measuredAt: Date.now(),
  };
}

function monotonicNow(): number {
  if (
    typeof performance !== "undefined" &&
    typeof performance.now === "function"
  ) {
    return performance.now();
  }
  return Date.now();
}

async function fetchWithTimeout(
  url: string,
  init: RequestInit,
  timeoutMs: number = LATENCY_TIMEOUT_MS,
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
