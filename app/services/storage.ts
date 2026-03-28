import AsyncStorage from '@react-native-async-storage/async-storage';

const KEYS = {
  serverUrl: 'zen:server_url',
  secret: 'zen:secret',
  nightMode: 'zen:night_mode',
  onboarded: 'zen:onboarded',
  terminalTheme: 'zen:terminal_theme',
} as const;

export type StoredTerminalTheme = 'zen-midnight' | 'zen-amber';

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
