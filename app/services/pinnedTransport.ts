import type { StoredServer, StoredTransportCandidate } from "./storage";

const selectionCache = new Map<
  string,
  {
    remoteURL: string;
    localOrigin: string;
    tunnelKey: string;
    measuredAt: number;
  }
>();
const SELECTION_TTL_MS = 30_000;

export async function resolveStoredServerURL(
  server: StoredServer,
): Promise<string> {
  if (server.transportKind !== "link") {
    return server.url;
  }
  if (
    !/^[0-9a-f]{64}$/i.test(server.transportPin || "") ||
    !/^[0-9a-f]{32}$/i.test(server.linkRouteId || "")
  ) {
    throw new Error(
      "Zen Link needs setup. Scan a fresh Zen Link QR code; the saved route or transport pin is missing.",
    );
  }
  const candidates = linkCandidates(server);
  const cacheKey = `server:${server.id}`;
  const cached = selectionCache.get(cacheKey);
  if (
    cached &&
    Date.now() - cached.measuredAt < SELECTION_TTL_MS &&
    candidates.some((candidate) => candidate.url === cached.remoteURL)
  ) {
    return rewriteToLocal(server.url, cached.remoteURL, cached.localOrigin);
  }

  const attempts = await Promise.all(
    candidates.map(async (candidate, index) => {
      const tunnelKey = `${cacheKey}:${index}`;
      try {
        const parsed = requireTLSServerURL(candidate.url);
        const result = await startNativePinnedTunnel(
          tunnelKey,
          parsed.hostname,
          parsed.port ? Number(parsed.port) : 443,
          server.transportPin!,
          "measure",
        );
        return {
          candidate,
          tunnelKey,
          localOrigin: `ws://127.0.0.1:${result.port}`,
          rttMs: result.rttMs,
        };
      } catch (error) {
        return { candidate, tunnelKey, error };
      }
    }),
  );
  const available = attempts
    .filter(
      (
        attempt,
      ): attempt is {
        candidate: StoredTransportCandidate;
        tunnelKey: string;
        localOrigin: string;
        rttMs: number;
      } => "localOrigin" in attempt,
    )
    .sort(
      (left, right) =>
        left.rttMs - right.rttMs ||
        left.candidate.name.localeCompare(right.candidate.name),
    );
  if (available.length === 0) {
    throw new Error(
      "Zen Link is offline. Keep zen running and check its Link connection.",
    );
  }
  const selected = available[0];
  selectionCache.set(cacheKey, {
    remoteURL: selected.candidate.url,
    localOrigin: selected.localOrigin,
    tunnelKey: selected.tunnelKey,
    measuredAt: Date.now(),
  });
  await Promise.all(
    available
      .slice(1)
      .map((attempt) =>
        stopNativePinnedTunnel(attempt.tunnelKey).catch(() => undefined),
      ),
  );
  return rewriteToLocal(
    server.url,
    selected.candidate.url,
    selected.localOrigin,
  );
}

export async function invalidateStoredServerTransport(
  server: StoredServer,
): Promise<void> {
  if (server.transportKind !== "link") {
    return;
  }
  const cached = selectionCache.get(`server:${server.id}`);
  selectionCache.delete(`server:${server.id}`);
  if (cached) {
    await stopNativePinnedTunnel(cached.tunnelKey).catch(() => undefined);
  }
}

export async function resolveCanonicalServerURL(
  server: StoredServer,
): Promise<string> {
  return resolveStoredServerURL(server);
}

export async function resolvePairingLinkURL(input: {
  routeId: string;
  transportPin: string;
  url: string;
}): Promise<string> {
  const parsed = requireTLSServerURL(input.url);
  let result: Awaited<ReturnType<typeof startNativePinnedTunnel>>;
  try {
    result = await startNativePinnedTunnel(
      `pair:${input.routeId}`,
      parsed.hostname,
      parsed.port ? Number(parsed.port) : 443,
      input.transportPin,
      "on-demand",
    );
  } catch {
    throw new Error(
      "Zen Link could not reach this computer. Keep zen running, wait for Link to show connected, then scan a fresh QR.",
    );
  }
  return rewriteURL(input.url, `ws://127.0.0.1:${result.port}`);
}

export async function releasePairingLinkTransport(
  routeId: string,
): Promise<void> {
  await stopNativePinnedTunnel(`pair:${routeId}`).catch(() => undefined);
}

async function startNativePinnedTunnel(
  key: string,
  host: string,
  port: number,
  pin: string,
  mode: "measure" | "on-demand",
) {
  const { startPinnedTunnel } = await import(
    "../modules/zen-link-transport/src"
  );
  return startPinnedTunnel(key, host, port, pin, mode);
}

async function stopNativePinnedTunnel(key: string): Promise<void> {
  const { stopPinnedTunnel } = await import(
    "../modules/zen-link-transport/src"
  );
  return stopPinnedTunnel(key);
}

function linkCandidates(server: StoredServer): StoredTransportCandidate[] {
  const configured = (server.transportCandidates || []).filter(
    (candidate) => candidate.kind === "link",
  );
  if (configured.length > 0) {
    return configured;
  }
  return [{ name: "Zen Link", kind: "link", url: server.url }];
}

function requireTLSServerURL(value: string): URL {
  const parsed = new URL(value);
  if (parsed.protocol !== "wss:") {
    throw new Error("Zen Link requires a wss:// candidate.");
  }
  return parsed;
}

function rewriteToLocal(
  requestedURL: string,
  selectedRemoteURL: string,
  localOrigin: string,
): string {
  const requested = new URL(requestedURL);
  const selected = new URL(selectedRemoteURL);
  if (requested.origin === selected.origin) {
    return rewriteURL(requestedURL, localOrigin);
  }
  return rewriteURL(selectedRemoteURL, localOrigin);
}

function rewriteURL(remoteURL: string, localOrigin: string): string {
  const remote = new URL(remoteURL);
  const local = new URL(localOrigin);
  remote.protocol = local.protocol;
  remote.hostname = local.hostname;
  remote.port = local.port;
  return remote.toString();
}
