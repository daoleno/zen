import type { AppColors } from './palette';

export type ThemeColorScheme = 'light' | 'dark';

/** Shell + list UI palette (tabs, settings, inbox). */
export type AppPalette = AppColors;

export type ChatLayout = 'chatgpt' | 'telegram' | 'classic';

/** Chat-specific palette — layout + colors for future theme picker support. */
export interface ChatPalette {
  layout: ChatLayout;
  showWallpaper: boolean;
  showTimestamps: boolean;
  showDateDividers: boolean;
  background: string;
  sentBubble: string;
  receivedBubble: string;
  sentText: string;
  receivedText: string;
  sentTimestamp: string;
  receivedTimestamp: string;
  composerBackground: string;
  composerBorder: string;
  composerDock: string;
  link: string;
  patternIcon: string;
}

export interface SurfacePalette {
  card: string;
  cardStrong: string;
  subtle: string;
  border: string;
  sectionLabel: string;
}

export interface ZenThemeDefinition {
  id: string;
  name: string;
  colorScheme: ThemeColorScheme;
  colors: AppPalette;
  chat: ChatPalette;
  surfaces: SurfacePalette;
  avatarColors: readonly string[];
}

export interface ResolvedZenTheme extends ZenThemeDefinition {
  isLight: boolean;
}

export type ThemePreference = 'system' | string;