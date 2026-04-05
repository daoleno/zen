import AsyncStorage from "@react-native-async-storage/async-storage";
import { parseSessionKey } from "./sessionKeys";
import { normalizeDaemonId, normalizePublicKeyHex } from "./auth";
import { normalizeServerURL as normalizeConnectionURL } from "./connection";

const KEYS = {
  servers: "zen:v3:servers",
  onboarded: "zen:onboarded",
  terminalTheme: "zen:terminal_theme",
  inboxViewMode: "zen:inbox_view_mode",
  recentAgentOpens: "zen:recent_agent_opens",
  terminalTabs: "zen:terminal_tabs",
  agentAliases: "zen:agent_aliases",
} as const;

export type StoredTerminalTheme = "zen-midnight" | "zen-amber";
export type StoredInboxViewMode = "list" | "grid";
export type StoredRecentAgentOpens = Record<string, number>;
export type StoredAgentAliases = Record<string, string>;
export interface StoredServer {
  id: string;
  name: string;
  url: string;
  daemonId: string;
  daemonPublicKey: string;
}
export interface StoredTerminalTabs {
  order: string[];
  pinned: string[];
}

const MAX_TERMINAL_TABS = 12;

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
    : servers.find((server) => server.daemonId === daemonId);

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
  await pruneTerminalTabsForServers([serverID]);
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

export async function getTerminalTheme(): Promise<StoredTerminalTheme> {
  const value = await AsyncStorage.getItem(KEYS.terminalTheme);
  return value === "zen-amber" ? "zen-amber" : "zen-midnight";
}

export async function setTerminalTheme(
  theme: StoredTerminalTheme,
): Promise<void> {
  await AsyncStorage.setItem(KEYS.terminalTheme, theme);
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

export async function getTerminalTabs(): Promise<StoredTerminalTabs> {
  const value = await AsyncStorage.getItem(KEYS.terminalTabs);
  if (!value) return { order: [], pinned: [] };

  try {
    const parsed = JSON.parse(value) as { order?: unknown; pinned?: unknown };
    return normalizeTerminalTabs({
      order: normalizeIdList(parsed.order),
      pinned: normalizeIdList(parsed.pinned),
    });
  } catch {
    return { order: [], pinned: [] };
  }
}

export async function touchTerminalTab(
  agentId: string,
): Promise<StoredTerminalTabs> {
  const current = await getTerminalTabs();
  const next = normalizeTerminalTabs({
    order: current.order.includes(agentId)
      ? current.order
      : [...current.order, agentId],
    pinned: current.pinned,
  });
  await AsyncStorage.setItem(KEYS.terminalTabs, JSON.stringify(next));
  return next;
}

export async function closeTerminalTab(
  agentId: string,
): Promise<StoredTerminalTabs> {
  const current = await getTerminalTabs();
  const next = normalizeTerminalTabs({
    order: current.order.filter((id) => id !== agentId),
    pinned: current.pinned.filter((id) => id !== agentId),
  });
  await AsyncStorage.setItem(KEYS.terminalTabs, JSON.stringify(next));
  return next;
}

export async function closeOtherTerminalTabs(
  agentId: string,
): Promise<StoredTerminalTabs> {
  const current = await getTerminalTabs();
  const next = normalizeTerminalTabs({
    order: [agentId],
    pinned: current.pinned.includes(agentId) ? [agentId] : [],
  });
  await AsyncStorage.setItem(KEYS.terminalTabs, JSON.stringify(next));
  return next;
}

export async function setTerminalTabPinned(
  agentId: string,
  pinned: boolean,
): Promise<StoredTerminalTabs> {
  const current = await getTerminalTabs();
  const next = normalizeTerminalTabs({
    order: current.order.includes(agentId)
      ? current.order
      : [...current.order, agentId],
    pinned: pinned
      ? [agentId, ...current.pinned.filter((id) => id !== agentId)]
      : current.pinned.filter((id) => id !== agentId),
  });
  await AsyncStorage.setItem(KEYS.terminalTabs, JSON.stringify(next));
  return next;
}

export async function syncTerminalTabsWithLiveSessions(
  liveSessionKeys: string[],
  hydratedServerIds: string[],
): Promise<StoredTerminalTabs> {
  const current = await getTerminalTabs();
  const liveSessions = new Set(liveSessionKeys);
  const hydratedServers = new Set(hydratedServerIds);

  const next = normalizeTerminalTabs({
    order: current.order.filter((sessionKey) =>
      shouldKeepSessionKey(sessionKey, liveSessions, hydratedServers),
    ),
    pinned: current.pinned.filter((sessionKey) =>
      shouldKeepSessionKey(sessionKey, liveSessions, hydratedServers),
    ),
  });

  if (
    next.order.length === current.order.length &&
    next.pinned.length === current.pinned.length &&
    next.order.every((value, index) => value === current.order[index]) &&
    next.pinned.every((value, index) => value === current.pinned[index])
  ) {
    return current;
  }

  await AsyncStorage.setItem(KEYS.terminalTabs, JSON.stringify(next));
  return next;
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

async function pruneTerminalTabsForServers(serverIds: string[]): Promise<void> {
  const blockedServers = new Set(serverIds);
  const current = await getTerminalTabs();
  const next = normalizeTerminalTabs({
    order: current.order.filter((sessionKey) => {
      const parsed = parseSessionKey(sessionKey);
      return parsed ? !blockedServers.has(parsed.serverId) : false;
    }),
    pinned: current.pinned.filter((sessionKey) => {
      const parsed = parseSessionKey(sessionKey);
      return parsed ? !blockedServers.has(parsed.serverId) : false;
    }),
  });

  await AsyncStorage.setItem(KEYS.terminalTabs, JSON.stringify(next));
}

function shouldKeepSessionKey(
  sessionKey: string,
  liveSessions: Set<string>,
  hydratedServers: Set<string>,
): boolean {
  const parsed = parseSessionKey(sessionKey);
  if (!parsed) return false;
  if (!hydratedServers.has(parsed.serverId)) return true;
  return liveSessions.has(sessionKey);
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

function normalizeTerminalTabs(state: StoredTerminalTabs): StoredTerminalTabs {
  const normalizedOrder = normalizeIdList(state.order);
  const normalizedPinned = normalizeIdList(state.pinned).filter((id) =>
    normalizedOrder.includes(id),
  );
  const order = trimTerminalTabOrder(normalizedOrder, normalizedPinned);
  const pinned = normalizedPinned.filter((id) => order.includes(id));
  return { order, pinned };
}

function trimTerminalTabOrder(order: string[], pinned: string[]): string[] {
  if (order.length <= MAX_TERMINAL_TABS) return order;

  const next = [...order];
  const pinnedSet = new Set(pinned);

  while (next.length > MAX_TERMINAL_TABS) {
    let removableIndex = -1;

    for (let index = next.length - 1; index >= 0; index -= 1) {
      if (pinnedSet.has(next[index])) continue;
      removableIndex = index;
      break;
    }

    if (removableIndex === -1) {
      next.length = MAX_TERMINAL_TABS;
      break;
    }

    next.splice(removableIndex, 1);
  }

  return next;
}
