import { Platform } from 'react-native';
import type { ResolvedZenTheme } from '../theme';
import { shadow } from './tokens';
import type { TerminalThemeChrome } from './terminalThemes';

export type ThemedSurfaces = {
  surface: string;
  surfaceStrong: string;
  subtle: string;
  border: string;
  sectionLabel: string;
};

export function surfacesFromTheme(theme: Pick<ResolvedZenTheme, 'surfaces'>): ThemedSurfaces {
  return {
    surface: theme.surfaces.card,
    surfaceStrong: theme.surfaces.cardStrong,
    subtle: theme.surfaces.subtle,
    border: theme.surfaces.border,
    sectionLabel: theme.surfaces.sectionLabel,
  };
}

/** @deprecated Pass ResolvedZenTheme via surfacesFromTheme(theme). */
export function createThemedSurfaces(
  theme: Pick<ResolvedZenTheme, 'surfaces'>,
): ThemedSurfaces {
  return surfacesFromTheme(theme);
}

export function isAmbientChatChrome(chrome: TerminalThemeChrome): boolean {
  return chrome.appBackground === 'transparent';
}

/** Translucent glass cards on SkyNatureBackdrop — Android elevation draws an opaque plate behind them. */
export function glassCardShadow(shadowColor: string) {
  if (Platform.OS === 'android') {
    return {};
  }
  return shadow('card', shadowColor);
}