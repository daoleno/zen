export type { AppColors } from './palette';
export type {
  AppPalette,
  ChatLayout,
  ChatPalette,
  ResolvedZenTheme,
  SurfacePalette,
  ThemeColorScheme,
  ThemePreference,
  ZenThemeDefinition,
} from './types';

export { ThemeProvider, useZenTheme } from './provider';
export { buildChatChrome } from './buildChatChrome';
export { resolveTheme } from './resolve';
export {
  DEFAULT_THEME_IDS,
  getThemeById,
  listThemesForScheme,
  THEME_REGISTRY,
} from './registry';
export { classicDarkTheme } from './definitions/classicDark';
export { classicLightTheme } from './definitions/classicLight';
export { mixHex, relativeLuminance } from './colorUtils';

import { classicDarkTheme } from './definitions/classicDark';
import { classicLightTheme } from './definitions/classicLight';

/** @deprecated Use THEME_REGISTRY or useAppColors() instead. */
export const LegacyDarkColors = classicDarkTheme.colors;

/** @deprecated Use THEME_REGISTRY or useAppColors() instead. */
export const LegacyLightColors = classicLightTheme.colors;