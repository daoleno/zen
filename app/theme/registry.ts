import { classicDarkTheme } from './definitions/classicDark';
import { classicLightTheme } from './definitions/classicLight';
import type { ThemeColorScheme, ZenThemeDefinition } from './types';

export const THEME_REGISTRY: readonly ZenThemeDefinition[] = [
  classicDarkTheme,
  classicLightTheme,
] as const;

export const DEFAULT_THEME_IDS: Record<ThemeColorScheme, string> = {
  dark: classicDarkTheme.id,
  light: classicLightTheme.id,
};

export function getThemeById(id: string): ZenThemeDefinition | undefined {
  return THEME_REGISTRY.find((theme) => theme.id === id);
}

export function listThemesForScheme(
  scheme: ThemeColorScheme,
): ZenThemeDefinition[] {
  return THEME_REGISTRY.filter((theme) => theme.colorScheme === scheme);
}