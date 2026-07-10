import { DEFAULT_THEME_IDS, getThemeById } from './registry';
import type { ResolvedZenTheme, ThemeColorScheme } from './types';

export function resolveTheme({
  colorScheme,
  themeId,
}: {
  colorScheme: ThemeColorScheme;
  themeId?: string | null;
}): ResolvedZenTheme {
  const fallbackId = DEFAULT_THEME_IDS[colorScheme];
  const requested = themeId ? getThemeById(themeId) : undefined;
  const definition = requested ?? getThemeById(fallbackId)!;

  return {
    ...definition,
    isLight: definition.colorScheme === 'light',
  };
}
