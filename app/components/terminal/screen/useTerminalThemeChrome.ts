import { useMemo } from "react";
import {
  buildTerminalChrome,
  isLightTerminalTheme,
  resolveTerminalTheme,
  resolveTerminalThemeName,
} from "../../../constants/terminalThemes";
import { useAppTheme } from "../../../constants/tokens";
import { buildChatChrome } from "../../../theme";

export function useTerminalThemeChrome() {
  const { theme: zenTheme } = useAppTheme();
  const themeName = useMemo(
    () => resolveTerminalThemeName(zenTheme.colorScheme),
    [zenTheme.colorScheme],
  );
  const terminalTheme = useMemo(
    () => resolveTerminalTheme(themeName),
    [themeName],
  );
  const chromeColors = useMemo(
    () => ({
      ...buildTerminalChrome(terminalTheme),
      appBackground: zenTheme.colors.bgPrimary,
      surface: zenTheme.colors.modalSurface,
      surfaceMuted: zenTheme.colors.modalSurfaceAlt,
      surfaceActive: zenTheme.colors.surfaceActive,
      composerInput: zenTheme.colors.inputBackground,
      border: zenTheme.colors.border,
      borderStrong: zenTheme.colors.borderStrong,
      text: zenTheme.colors.textPrimary,
      textMuted: zenTheme.colors.textSecondary,
      textSubtle: zenTheme.colors.textTertiary,
      textOnAccent: zenTheme.colors.textOnAccent,
      accent: zenTheme.colors.accent,
      accentSoft: zenTheme.colors.accentSoft,
      disabledSurface: zenTheme.colors.disabledSurface,
      focus: zenTheme.colors.focusRing,
      link: zenTheme.colors.accentStrong,
      danger: zenTheme.colors.dangerText,
      dangerSoft: zenTheme.colors.dangerSoft,
      overlay: zenTheme.colors.modalBackdrop,
      shadowColor: zenTheme.colors.shadowColor,
    }),
    [terminalTheme, zenTheme.colors],
  );
  const chat = useMemo(() => buildChatChrome(zenTheme), [zenTheme]);
  const statusBarStyle: "dark" | "light" = isLightTerminalTheme(terminalTheme)
    ? "dark"
    : "light";

  return {
    chromeColors,
    chatChrome: chat.chrome,
    chatTheme: chat.theme,
    statusBarStyle,
    terminalTheme,
  };
}
