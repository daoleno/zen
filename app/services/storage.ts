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
  recentWorkerOpens: "zen:recent_agent_opens",
  workerAliases: "zen:agent_aliases",
  interfaceRenderModes: "zen:codex_render_modes",
  themePreference: "zen:theme_preference",
} as const;

export type StoredThemePreference = "system" | string;

export type StoredRecentWorkerOpens = Record<string, number>;
export type StoredWorkerAliases = Record<string, string>;
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

export async function getRecentWorkerOpens(): Promise<StoredRecentWorkerOpens> {
  const value = await AsyncStorage.getItem(KEYS.recentWorkerOpens);
  if (!value) return {};

  try {
    const parsed = JSON.parse(value) as Record<string, unknown>;
    const normalized: StoredRecentWorkerOpens = {};
    for (const [workerId, openedAt] of Object.entries(parsed)) {
      if (typeof openedAt === "number" && Number.isFinite(openedAt)) {
        normalized[workerId] = openedAt;
      }
    }
    return normalized;
  } catch {
    return {};
  }
}

export async function markWorkerOpened(
  workerId: string,
  openedAt: number = Date.now(),
): Promise<void> {
  const current = await getRecentWorkerOpens();
  const next: StoredRecentWorkerOpens = {
    ...current,
    [workerId]: openedAt,
  };

  const entries = Object.entries(next)
    .sort((left, right) => right[1] - left[1])
    .slice(0, 100);

  await AsyncStorage.setItem(
    KEYS.recentWorkerOpens,
    JSON.stringify(Object.fromEntries(entries)),
  );
}

export async function getWorkerAliases(): Promise<StoredWorkerAliases> {
  const value = await AsyncStorage.getItem(KEYS.workerAliases);
  if (!value) return {};

  try {
    const parsed = JSON.parse(value) as Record<string, unknown>;
    const normalized: StoredWorkerAliases = {};
    for (const [workerId, alias] of Object.entries(parsed)) {
      if (typeof workerId !== "string" || workerId.trim().length === 0) continue;
      if (typeof alias !== "string") continue;

      const trimmed = alias.trim();
      if (!trimmed) continue;
      normalized[workerId] = trimmed;
    }
    return normalized;
  } catch {
    return {};
  }
}

export async function setWorkerAlias(
  workerId: string,
  alias: string,
): Promise<StoredWorkerAliases> {
  const current = await getWorkerAliases();
  const next: StoredWorkerAliases = { ...current };
  const trimmed = alias.trim();

  if (trimmed) {
    next[workerId] = trimmed;
  } else {
    delete next[workerId];
  }

  await AsyncStorage.setItem(KEYS.workerAliases, JSON.stringify(next));
  return next;
}

export async function getInterfaceRenderModes(): Promise<StoredInterfaceRenderModes> {
  const value = await AsyncStorage.getItem(KEYS.interfaceRenderModes);
  if (!value) return {};

  try {
    const parsed = JSON.parse(value) as Record<string, unknown>;
    const normalized: StoredInterfaceRenderModes = {};
    for (const [workerId, mode] of Object.entries(parsed)) {
      if (typeof workerId !== "string" || workerId.trim().length === 0) continue;
      if (mode === "chat" || mode === "terminal") {
        normalized[workerId] = mode;
      }
    }
    return normalized;
  } catch {
    return {};
  }
}

export async function setInterfaceRenderMode(
  workerId: string,
  mode: StoredInterfaceRenderMode,
): Promise<StoredInterfaceRenderModes> {
  const trimmed = workerId.trim();
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
