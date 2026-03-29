import AsyncStorage from '@react-native-async-storage/async-storage';

const KEYS = {
  serverUrl: 'zen:server_url',
  secret: 'zen:secret',
  nightMode: 'zen:night_mode',
  onboarded: 'zen:onboarded',
  terminalTheme: 'zen:terminal_theme',
  inboxViewMode: 'zen:inbox_view_mode',
  recentAgentOpens: 'zen:recent_agent_opens',
} as const;

export type StoredTerminalTheme = 'zen-midnight' | 'zen-amber';
export type StoredInboxViewMode = 'list' | 'grid';
export type StoredRecentAgentOpens = Record<string, number>;

export async function getServerUrl(): Promise<string> {
  return (await AsyncStorage.getItem(KEYS.serverUrl)) || '';
}

export async function setServerUrl(url: string): Promise<void> {
  await AsyncStorage.setItem(KEYS.serverUrl, url);
}

export async function getSecret(): Promise<string> {
  return (await AsyncStorage.getItem(KEYS.secret)) || '';
}

export async function setSecret(secret: string): Promise<void> {
  await AsyncStorage.setItem(KEYS.secret, secret);
}

export async function getNightMode(): Promise<boolean> {
  return (await AsyncStorage.getItem(KEYS.nightMode)) === 'true';
}

export async function setNightMode(enabled: boolean): Promise<void> {
  await AsyncStorage.setItem(KEYS.nightMode, String(enabled));
}

export async function isOnboarded(): Promise<boolean> {
  return (await AsyncStorage.getItem(KEYS.onboarded)) === 'true';
}

export async function markOnboarded(): Promise<void> {
  await AsyncStorage.setItem(KEYS.onboarded, 'true');
}

export async function getTerminalTheme(): Promise<StoredTerminalTheme> {
  const value = await AsyncStorage.getItem(KEYS.terminalTheme);
  return value === 'zen-amber' ? 'zen-amber' : 'zen-midnight';
}

export async function setTerminalTheme(theme: StoredTerminalTheme): Promise<void> {
  await AsyncStorage.setItem(KEYS.terminalTheme, theme);
}

export async function getInboxViewMode(): Promise<StoredInboxViewMode> {
  const value = await AsyncStorage.getItem(KEYS.inboxViewMode);
  return value === 'grid' ? 'grid' : 'list';
}

export async function setInboxViewMode(mode: StoredInboxViewMode): Promise<void> {
  await AsyncStorage.setItem(KEYS.inboxViewMode, mode);
}

export async function getRecentAgentOpens(): Promise<StoredRecentAgentOpens> {
  const value = await AsyncStorage.getItem(KEYS.recentAgentOpens);
  if (!value) return {};

  try {
    const parsed = JSON.parse(value) as Record<string, unknown>;
    const normalized: StoredRecentAgentOpens = {};
    for (const [agentId, openedAt] of Object.entries(parsed)) {
      if (typeof openedAt === 'number' && Number.isFinite(openedAt)) {
        normalized[agentId] = openedAt;
      }
    }
    return normalized;
  } catch {
    return {};
  }
}

export async function markAgentOpened(agentId: string, openedAt: number = Date.now()): Promise<void> {
  const current = await getRecentAgentOpens();
  const next: StoredRecentAgentOpens = {
    ...current,
    [agentId]: openedAt,
  };

  const entries = Object.entries(next)
    .sort((left, right) => right[1] - left[1])
    .slice(0, 100);

  await AsyncStorage.setItem(KEYS.recentAgentOpens, JSON.stringify(Object.fromEntries(entries)));
}
