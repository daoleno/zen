import AsyncStorage from "@react-native-async-storage/async-storage";
import {
  mergeStoredServer,
  normalizeStoredServers,
  type ServerTransportKind,
  type StoredServer,
  type StoredServerInput,
  type StoredTransportCandidate,
} from "./storedServerContract";

export type {
  ServerTransportKind,
  StoredServer,
  StoredTransportCandidate,
} from "./storedServerContract";

const KEYS = {
  servers: "zen:v3:servers",
  currentServer: "zen:v1:current_server_id",
  disabledServers: "zen:v1:disabled_servers",
  onboarded: "zen:onboarded",
  recentAgentOpens: "zen:recent_agent_opens",
  agentAliases: "zen:agent_aliases",
  interfaceRenderModes: "zen:codex_render_modes",
  themePreference: "zen:theme_preference",
} as const;

export type StoredThemePreference = "system" | string;

export type StoredRecentAgentOpens = Record<string, number>;
export type StoredAgentAliases = Record<string, string>;
export type StoredInterfaceRenderMode = "chat" | "terminal";
export type StoredInterfaceRenderModes = Record<
  string,
  StoredInterfaceRenderMode
>;
export async function getServers(): Promise<StoredServer[]> {
  const value = await AsyncStorage.getItem(KEYS.servers);
  if (!value) return [];

  try {
    return normalizeStoredServers(JSON.parse(value) as unknown);
  } catch {
    return [];
  }
}

export async function saveServer(
  input: StoredServerInput,
): Promise<StoredServer> {
  const servers = await getServers();
  const next = mergeStoredServer(input, servers, createServerID);
  await AsyncStorage.setItem(KEYS.servers, JSON.stringify(next.servers));
  return next.server;
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

export async function getCurrentServerId(): Promise<string | null> {
  const value = (await AsyncStorage.getItem(KEYS.currentServer))?.trim() || "";
  return value || null;
}

export async function setCurrentServerId(
  serverId: string | null,
): Promise<void> {
  const normalizedId = serverId?.trim() || "";
  if (!normalizedId) {
    await AsyncStorage.removeItem(KEYS.currentServer);
    return;
  }
  await AsyncStorage.setItem(KEYS.currentServer, normalizedId);
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

export async function getInterfaceRenderModes(): Promise<StoredInterfaceRenderModes> {
  const value = await AsyncStorage.getItem(KEYS.interfaceRenderModes);
  if (!value) return {};

  try {
    const parsed = JSON.parse(value) as Record<string, unknown>;
    const normalized: StoredInterfaceRenderModes = {};
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

export async function setInterfaceRenderMode(
  agentId: string,
  mode: StoredInterfaceRenderMode,
): Promise<StoredInterfaceRenderModes> {
  const trimmed = agentId.trim();
  if (!trimmed) {
    return getInterfaceRenderModes();
  }

  const current = await getInterfaceRenderModes();
  const next: StoredInterfaceRenderModes = {
    ...current,
    [trimmed]: mode,
  };

  await AsyncStorage.setItem(KEYS.interfaceRenderModes, JSON.stringify(next));
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
