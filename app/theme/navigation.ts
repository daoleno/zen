import type { ResolvedZenTheme } from "./types";

type ExpoNavigationTheme = typeof import("expo-router").DefaultTheme;

export type NavigationThemeFonts = ExpoNavigationTheme["fonts"];

/**
 * Gives every React Navigation-owned surface the already-resolved Zen theme.
 * Fonts are supplied separately because they are scheme-neutral; no navigation
 * color is inherited from React Navigation's default theme.
 */
export function navigationThemeFromZenTheme(
  theme: ResolvedZenTheme,
  fonts: NavigationThemeFonts,
): ExpoNavigationTheme {
  const { colors } = theme;

  return {
    dark: theme.colorScheme === "dark",
    colors: {
      primary: colors.accent,
      background: colors.bgPrimary,
      card: colors.bgSurface,
      text: colors.textPrimary,
      border: colors.border,
      notification: colors.statusFailed,
    },
    fonts,
  };
}
