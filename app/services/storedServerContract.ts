export type ServerTransportKind = "manual" | "link";

export interface StoredTransportCandidate {
  name: string;
  kind: ServerTransportKind;
  url: string;
}

export interface StoredServer {
  id: string;
  name: string;
  url: string;
  daemonId: string;
  daemonPublicKey: string;
  transportKind?: ServerTransportKind;
  transportPin?: string;
  linkRouteId?: string;
  transportCandidates?: StoredTransportCandidate[];
}

export interface StoredServerInput {
  id?: string;
  name: string;
  url: string;
  daemonId: string;
  daemonPublicKey: string;
  transportKind?: ServerTransportKind;
  transportPin?: string;
  linkRouteId?: string;
  transportCandidates?: StoredTransportCandidate[];
}

export function normalizeStoredServers(value: unknown): StoredServer[] {
  if (!Array.isArray(value)) return [];

  const normalized: StoredServer[] = [];
  for (const item of value) {
    if (!item || typeof item !== "object") continue;
    const candidate = item as Record<string, unknown>;
    const id = typeof candidate.id === "string" ? candidate.id.trim() : "";
    const rawName =
      typeof candidate.name === "string" ? candidate.name.trim() : "";
    const rawURL =
      typeof candidate.url === "string"
        ? normalizeServerURL(candidate.url)
        : "";
    const daemonId = normalizeHex(candidate.daemonId, 64) || "";
    const daemonPublicKey = normalizeHex(candidate.daemonPublicKey, 64) || "";
    if (!id || !rawURL || !daemonId || !daemonPublicKey) continue;

    const transportKind =
      candidate.transportKind === "link" ? "link" : "manual";
    const transportPin =
      transportKind === "link"
        ? normalizeHex(candidate.transportPin, 64)
        : undefined;
    const linkRouteId =
      transportKind === "link"
        ? normalizeHex(candidate.linkRouteId, 32)
        : undefined;
    if (transportKind === "link" && (!transportPin || !linkRouteId)) {
      continue;
    }

    normalized.push({
      id,
      name: rawName || deriveServerName(rawURL),
      url: rawURL,
      daemonId,
      daemonPublicKey,
      transportKind,
      transportPin,
      linkRouteId,
      transportCandidates: normalizeTransportCandidates(
        candidate.transportCandidates,
        transportKind,
        rawURL,
      ),
    });
  }

  return dedupeServers(normalized);
}

export function mergeStoredServer(
  input: StoredServerInput,
  servers: StoredServer[],
  createServerID: () => string,
): { server: StoredServer; servers: StoredServer[] } {
  const normalizedURL = normalizeServerURL(input.url);
  if (!normalizedURL) {
    throw new Error("Invalid server URL.");
  }
  const daemonId = normalizeHex(input.daemonId, 64) || "";
  const daemonPublicKey = normalizeHex(input.daemonPublicKey, 64) || "";
  if (!daemonId || !daemonPublicKey) {
    throw new Error("Missing daemon identity.");
  }

  const transportKind: ServerTransportKind =
    input.transportKind === "link" ? "link" : "manual";
  const transportPin =
    transportKind === "link" ? normalizeHex(input.transportPin, 64) : undefined;
  const linkRouteId =
    transportKind === "link" ? normalizeHex(input.linkRouteId, 32) : undefined;
  if (transportKind === "link" && (!transportPin || !linkRouteId)) {
    throw new Error("Missing Zen Link transport identity.");
  }

  const existingMatch = input.id?.trim()
    ? null
    : servers.find(
        (server) =>
          server.daemonId === daemonId &&
          server.daemonPublicKey === daemonPublicKey &&
          (transportKind === "link" || server.url === normalizedURL),
      );
  const server: StoredServer = {
    id: input.id?.trim() || existingMatch?.id || createServerID(),
    name:
      input.name.trim() ||
      existingMatch?.name ||
      deriveServerName(normalizedURL),
    url: normalizedURL,
    daemonId,
    daemonPublicKey,
    transportKind,
    transportPin,
    linkRouteId,
    transportCandidates: normalizeTransportCandidates(
      input.transportCandidates,
      transportKind,
      normalizedURL,
    ),
  };

  return {
    server,
    servers: dedupeServers([
      server,
      ...servers.filter((candidate) => candidate.id !== server.id),
    ]),
  };
}

export function normalizeServerURL(rawValue: string): string {
  const trimmed = rawValue.trim();
  if (!trimmed) return "";

  try {
    const parsed = new URL(trimmed);
    switch (parsed.protocol) {
      case "http:":
        parsed.protocol = "ws:";
        break;
      case "https:":
        parsed.protocol = "wss:";
        break;
      case "ws:":
      case "wss:":
        break;
      default:
        return "";
    }

    if (parsed.pathname === "" || parsed.pathname === "/") {
      parsed.pathname = "/ws";
    }
    parsed.search = "";
    parsed.hash = "";
    return parsed.toString();
  } catch {
    return "";
  }
}

function normalizeTransportCandidates(
  value: unknown,
  fallbackKind: ServerTransportKind,
  fallbackURL: string,
): StoredTransportCandidate[] {
  const normalized: StoredTransportCandidate[] = [];
  if (Array.isArray(value)) {
    for (const raw of value) {
      if (!raw || typeof raw !== "object") continue;
      const candidate = raw as Record<string, unknown>;
      const url =
        typeof candidate.url === "string"
          ? normalizeServerURL(candidate.url)
          : "";
      if (!url || normalized.some((entry) => entry.url === url)) continue;
      normalized.push({
        name:
          typeof candidate.name === "string" && candidate.name.trim()
            ? candidate.name.trim()
            : deriveServerName(url),
        kind: candidate.kind === "link" ? "link" : fallbackKind,
        url,
      });
    }
  }
  if (!normalized.some((candidate) => candidate.url === fallbackURL)) {
    normalized.unshift({
      name: fallbackKind === "link" ? "Zen Link" : "Manual",
      kind: fallbackKind,
      url: fallbackURL,
    });
  }
  return normalized;
}

function normalizeHex(value: unknown, length: number): string | undefined {
  if (typeof value !== "string") return undefined;
  const normalized = value.trim().toLowerCase();
  return new RegExp(`^[0-9a-f]{${length}}$`).test(normalized)
    ? normalized
    : undefined;
}

function dedupeServers(servers: StoredServer[]): StoredServer[] {
  const seen = new Set<string>();
  const normalized: StoredServer[] = [];
  for (const server of servers) {
    if (!server.id || !server.url || seen.has(server.id)) continue;
    seen.add(server.id);
    normalized.push(server);
  }
  return normalized;
}

function deriveServerName(url: string): string {
  try {
    const parsed = new URL(url);
    return parsed.hostname || url;
  } catch {
    return url;
  }
}
