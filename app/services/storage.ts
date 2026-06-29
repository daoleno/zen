import AsyncStorage from "@react-native-async-storage/async-storage";
import { normalizeDaemonId, normalizePublicKeyHex } from "./auth";
import { normalizeServerURL as normalizeConnectionURL } from "./connection";

const KEYS = {
  servers: "zen:v3:servers",
  disabledServers: "zen:v1:disabled_servers",
  onboarded: "zen:onboarded",
  inboxViewMode: "zen:inbox_view_mode",
  recentAgentOpens: "zen:recent_agent_opens",
  agentAliases: "zen:agent_aliases",
  codexRenderModes: "zen:codex_render_modes",
  themePreference: "zen:theme_preference",
} as const;

export type StoredThemePreference = "system" | string;

export type StoredInboxViewMode = "list" | "grid";
export type StoredRecentAgentOpens = Record<string, number>;
export type StoredAgentAliases = Record<string, string>;
export type StoredCodexRenderMode = "chat" | "terminal";
export type StoredCodexRenderModes = Record<string, StoredCodexRenderMode>;
export const DefaultCodexRenderMode: StoredCodexRenderMode = "chat";
export interface StoredServer {
  id: string;
  name: string;
  url: string;
  daemonId: string;
  daemonPublicKey: string;
}

export async function getServers(): Promise<StoredServer[]> {
  const value = await AsyncStorage.getItem(KEYS.servers);
  if (!value) return [];

  try {
    const parsed = JSON.parse(value) as unknown;
    if (!Array.isArray(parsed)) return [];

    const normalized: StoredServer[] = [];
    for (const item of parsed) {
      if (!item || typeof item !== "object") continue;
      const candidate = item as Record<string, unknown>;
      const id = typeof candidate.id === "string" ? candidate.id.trim() : "";
      const rawName =
        typeof candidate.name === "string" ? candidate.name.trim() : "";
      const rawURL =
        typeof candidate.url === "string"
          ? normalizeConnectionURL(candidate.url)
          : "";
      const daemonId =
        typeof candidate.daemonId === "string"
          ? normalizeDaemonId(candidate.daemonId)
          : "";
      const daemonPublicKey =
        typeof candidate.daemonPublicKey === "string"
          ? normalizePublicKeyHex(candidate.daemonPublicKey)
          : "";
      if (!id || !rawURL || !daemonId || !daemonPublicKey) continue;

      normalized.push({
        id,
        name: rawName || deriveServerName(rawURL),
        url: rawURL,
        daemonId,
        daemonPublicKey,
      });
    }

    return dedupeServers(normalized);
  } catch {
    return [];
  }
}

export async function saveServer(input: {
  id?: string;
  name: string;
  url: string;
  daemonId: string;
  daemonPublicKey: string;
}): Promise<StoredServer> {
  const servers = await getServers();
  const normalizedURL = normalizeConnectionURL(input.url);
  if (!normalizedURL) {
    throw new Error("Invalid server URL.");
  }
  const normalizedName = input.name.trim() || deriveServerName(normalizedURL);
  const daemonId = normalizeDaemonId(input.daemonId);
  const daemonPublicKey = normalizePublicKeyHex(input.daemonPublicKey);
  if (!daemonId || !daemonPublicKey) {
    throw new Error("Missing daemon identity.");
  }
  const existingMatch = input.id?.trim()
    ? null
    : servers.find(
        (server) =>
          server.daemonId === daemonId &&
          server.daemonPublicKey === daemonPublicKey &&
          server.url === normalizedURL,
      );

  const nextServer: StoredServer = {
    id: input.id?.trim() || existingMatch?.id || createServerID(),
    name: normalizedName,
    url: normalizedURL,
    daemonId,
    daemonPublicKey,
  };

  const nextServers = dedupeServers([
    nextServer,
    ...servers.filter((server) => server.id !== nextServer.id),
  ]);

  await AsyncStorage.setItem(KEYS.servers, JSON.stringify(nextServers));
  return nextServer;
}

export async function removeServer(serverID: string): Promise<void> {
  const servers = await getServers();
  const nextServers = servers.filter((server) => server.id !== serverID);
  await AsyncStorage.setItem(KEYS.servers, JSON.stringify(nextServers));
  await setServerAutoConnect(serverID, true);
}

export async function getServerById(
  serverId: string,
): Promise<StoredServer | null> {
  const servers = await getServers();
  return servers.find((server) => server.id === serverId) || null;
}

export async function isOnboarded(): Promise<boolean> {
  return (await AsyncStorage.getItem(KEYS.onboarded)) === "true";
}

export async function markOnboarded(): Promise<void> {
  await AsyncStorage.setItem(KEYS.onboarded, "true");
}

export async function getDisabledServerIds(): Promise<string[]> {
  const value = await AsyncStorage.getItem(KEYS.disabledServers);
  if (!value) return [];

  try {
    return normalizeIdList(JSON.parse(value));
  } catch {
    return [];
  }
}

export async function setServerAutoConnect(
  serverId: string,
  enabled: boolean,
): Promise<void> {
  const normalizedId = serverId.trim();
  if (!normalizedId) {
    return;
  }

  const current = await getDisabledServerIds();
  const next = enabled
    ? current.filter((id) => id !== normalizedId)
    : normalizeIdList([...current, normalizedId]);

  if (next.length === 0) {
    await AsyncStorage.removeItem(KEYS.disabledServers);
    return;
  }

  await AsyncStorage.setItem(KEYS.disabledServers, JSON.stringify(next));
}

export async function getInboxViewMode(): Promise<StoredInboxViewMode> {
  const value = await AsyncStorage.getItem(KEYS.inboxViewMode);
  return value === "grid" ? "grid" : "list";
}

export async function setInboxViewMode(
  mode: StoredInboxViewMode,
): Promise<void> {
  await AsyncStorage.setItem(KEYS.inboxViewMode, mode);
}

export async function getRecentAgentOpens(): Promise<StoredRecentAgentOpens> {
  const value = await AsyncStorage.getItem(KEYS.recentAgentOpens);
  if (!value) return {};

  try {
    const parsed = JSON.parse(value) as Record<string, unknown>;
    const normalized: StoredRecentAgentOpens = {};
    for (const [agentId, openedAt] of Object.entries(parsed)) {
      if (typeof openedAt === "number" && Number.isFinite(openedAt)) {
        normalized[agentId] = openedAt;
      }
    }
    return normalized;
  } catch {
    return {};
  }
}

export async function markAgentOpened(
  agentId: string,
  openedAt: number = Date.now(),
): Promise<void> {
  const current = await getRecentAgentOpens();
  const next: StoredRecentAgentOpens = {
    ...current,
    [agentId]: openedAt,
  };

  const entries = Object.entries(next)
    .sort((left, right) => right[1] - left[1])
    .slice(0, 100);

  await AsyncStorage.setItem(
    KEYS.recentAgentOpens,
    JSON.stringify(Object.fromEntries(entries)),
  );
}

export async function getAgentAliases(): Promise<StoredAgentAliases> {
  const value = await AsyncStorage.getItem(KEYS.agentAliases);
  if (!value) return {};

  try {
    const parsed = JSON.parse(value) as Record<string, unknown>;
    const normalized: StoredAgentAliases = {};
    for (const [agentId, alias] of Object.entries(parsed)) {
      if (typeof agentId !== "string" || agentId.trim().length === 0) continue;
      if (typeof alias !== "string") continue;

      const trimmed = alias.trim();
      if (!trimmed) continue;
      normalized[agentId] = trimmed;
    }
    return normalized;
  } catch {
    return {};
  }
}

export async function setAgentAlias(
  agentId: string,
  alias: string,
): Promise<StoredAgentAliases> {
  const current = await getAgentAliases();
  const next: StoredAgentAliases = { ...current };
  const trimmed = alias.trim();

  if (trimmed) {
    next[agentId] = trimmed;
  } else {
    delete next[agentId];
  }

  await AsyncStorage.setItem(KEYS.agentAliases, JSON.stringify(next));
  return next;
}

export async function getCodexRenderModes(): Promise<StoredCodexRenderModes> {
  const value = await AsyncStorage.getItem(KEYS.codexRenderModes);
  if (!value) return {};

  try {
    const parsed = JSON.parse(value) as Record<string, unknown>;
    const normalized: StoredCodexRenderModes = {};
    for (const [agentId, mode] of Object.entries(parsed)) {
      if (typeof agentId !== "string" || agentId.trim().length === 0) continue;
      if (mode === "chat" || mode === "terminal") {
        normalized[agentId] = mode;
      }
    }
    return normalized;
  } catch {
    return {};
  }
}

export async function setCodexRenderMode(
  agentId: string,
  mode: StoredCodexRenderMode,
): Promise<StoredCodexRenderModes> {
  const trimmed = agentId.trim();
  if (!trimmed) {
    return getCodexRenderModes();
  }

  const current = await getCodexRenderModes();
  const next: StoredCodexRenderModes = {
    ...current,
    [trimmed]: mode,
  };

  await AsyncStorage.setItem(KEYS.codexRenderModes, JSON.stringify(next));
  return next;
}

function normalizeIdList(value: unknown): string[] {
  if (!Array.isArray(value)) return [];

  const normalized: string[] = [];
  for (const item of value) {
    if (
      typeof item !== "string" ||
      item.trim().length === 0 ||
      normalized.includes(item)
    )
      continue;
    normalized.push(item);
  }
  return normalized;
}

function createServerID(): string {
  return `server_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
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

export async function getThemePreference(): Promise<StoredThemePreference | null> {
  const value = await AsyncStorage.getItem(KEYS.themePreference);
  if (!value) return null;
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : null;
}

export async function setThemePreference(
  preference: StoredThemePreference,
): Promise<void> {
  await AsyncStorage.setItem(KEYS.themePreference, preference);
}
