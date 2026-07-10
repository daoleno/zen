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
    textOnAccent: colors.textOnAccent,
    accent: colors.accent,
    accentSoft: colors.accentSoft,
    disabledSurface: colors.disabledSurface,
    focus: colors.focusRing,
    link: chat.link,
    danger: colors.dangerText,
    dangerSoft: colors.dangerSoft,
    overlay: colors.modalBackdrop,
    shadowColor: colors.shadowColor,
  };

  const palette: TerminalThemePalette = {
    ...baseTheme,
    background: chat.background,
    foreground: chat.receivedText,
    cursor: colors.accent,
    cursorAccent: theme.isLight ? colors.textOnAccent : chat.background,
    selectionBackground: colors.selectionBackground,
  };

  return { chrome, theme: palette };
}
