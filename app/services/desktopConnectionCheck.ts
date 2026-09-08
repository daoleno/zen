import type { DaemonAssertionInput } from "./auth";
import type { StoredServer } from "./storedServerContract";

export interface DesktopProofDependencies {
  fetch: (url: string, init: Pick<RequestInit, "headers" | "signal" | "redirect">) => Promise<Pick<Response, "ok" | "status" | "url" | "redirected" | "body">>;
  authorization: () => Promise<string>;
  verify: (input: DaemonAssertionInput) => boolean;
  timeoutMs?: number;
}

export async function verifyDesktopServer(server: Pick<StoredServer, "daemonId" | "daemonPublicKey">, desktop: string,
  dependencies: DesktopProofDependencies, signal?: AbortSignal): Promise<void> {
  const endpoint = new URL(desktop);
  endpoint.protocol = endpoint.protocol === "wss:" ? "https:" : "http:";
  for (const purpose of ["zen-health", "zen-probe"] as const) {
    endpoint.pathname = purpose === "zen-health" ? "/health" : "/auth-check";
    const controller = new AbortController();
    const abort = () => controller.abort();
    signal?.addEventListener("abort", abort);
    const timer = setTimeout(abort, dependencies.timeoutMs ?? 5000);
    try {
      if (signal?.aborted) throw new Error("Desktop connection cancelled.");
      const headers = purpose === "zen-probe" ? { Authorization: await dependencies.authorization() } : undefined;
      if (signal?.aborted) throw new Error("Desktop connection cancelled.");
      const response = await dependencies.fetch(endpoint.toString(), { headers, signal: controller.signal, redirect: "error" });
      if (response.redirected || (response.url && response.url !== endpoint.toString())) throw new Error("Desktop endpoint redirects are not allowed.");
      if (!response.ok) throw new Error(response.status === 401 ? "This device is no longer paired with the computer." : "The desktop server did not pass its connection check.");
      if (!response.body) throw new Error("The desktop server returned no identity proof.");
      const reader = response.body.getReader();
      const chunks: Uint8Array[] = []; let size = 0;
      try {
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          size += value.byteLength;
          if (size > 8192) { controller.abort(); throw new Error("The desktop identity response is too large."); }
          chunks.push(value);
        }
      } finally { reader.releaseLock(); }
      const bytes = new Uint8Array(size); let offset = 0;
      for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
      const payload = JSON.parse(new TextDecoder().decode(bytes));
      if (!payload || typeof payload !== "object") throw new Error("The desktop server returned an invalid identity proof.");
      const timestamp = Date.parse(payload.assertion_timestamp);
      if (payload.daemon_id !== server.daemonId || payload.daemon_public_key !== server.daemonPublicKey ||
          (purpose === "zen-probe" && payload.ok !== true) ||
          !Number.isFinite(timestamp) || Math.abs(Date.now() - timestamp) > 300000 ||
          !dependencies.verify({ purpose, daemonId: server.daemonId, daemonPublicKey: server.daemonPublicKey,
            timestamp: payload.assertion_timestamp, nonceHex: payload.assertion_nonce, signatureHex: payload.assertion_signature })) {
        throw new Error("The endpoint did not prove the identity of this paired computer.");
      }
    } finally { clearTimeout(timer); signal?.removeEventListener("abort", abort); }
  }
}
