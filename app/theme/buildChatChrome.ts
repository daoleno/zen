import {
  resolveTerminalTheme,
  type TerminalThemeChrome,
  type TerminalThemePalette,
} from '../constants/terminalThemes';
import type { ResolvedZenTheme } from './types';

export function buildChatChrome(theme: ResolvedZenTheme): {
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
} {
  const baseTheme = resolveTerminalTheme(theme.isLight ? 'light' : 'dark');
  const { colors, chat } = theme;

  const chrome: TerminalThemeChrome = {
    appBackground: chat.background,
    surface: chat.receivedBubble,
    surfaceMuted: chat.sentBubble,
    surfaceActive:
      chat.composerDock === 'transparent' ? chat.composerBackground : chat.composerDock,
    composerInput: chat.composerBackground,
    border: chat.composerBorder,
    borderStrong: colors.borderStrong,
    text: chat.receivedText,
    textMuted: colors.textSecondary,
    textSubtle: colors.textTertiary,
    accent: colors.accent,
    accentSoft: colors.accentSoft,
    overlay: colors.modalBackdrop,
  };

  const palette: TerminalThemePalette = {
    ...baseTheme,
    background: chat.background,
    foreground: chat.receivedText,
    cursor: colors.accent,
    cursorAccent: theme.isLight ? colors.textOnAccent : chat.background,
  };

  return { chrome, theme: palette };
}