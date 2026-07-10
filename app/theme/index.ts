export type { AppColors } from './palette';
export type {
  AppPalette,
  ChatLayout,
  ChatPalette,
  DataVisualizationPalette,
  ResolvedZenTheme,
  SurfacePalette,
  ThemeColorScheme,
  ThemePreference,
  ZenThemeDefinition,
} from './types';

export {
  ZEN_BRAND_COLORS,
  ZEN_DARK_APP_COLORS,
  ZEN_DARK_CHAT_PALETTE,
  ZEN_DARK_DATA_VISUALIZATION,
  ZEN_DARK_NEUTRALS,
  ZEN_DARK_OVERLAYS,
  ZEN_DARK_STATUS,
  ZEN_DARK_SURFACE_PALETTE,
  ZEN_LIGHT_APP_COLORS,
  ZEN_LIGHT_CHAT_PALETTE,
  ZEN_LIGHT_DATA_VISUALIZATION,
  ZEN_LIGHT_NEUTRALS,
  ZEN_LIGHT_OVERLAYS,
  ZEN_LIGHT_STATUS,
  ZEN_LIGHT_SURFACE_PALETTE,
  ZEN_SAGE,
} from './primitives';

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
