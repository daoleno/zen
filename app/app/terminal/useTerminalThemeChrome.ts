import { useMemo } from "react";
import { useColorScheme } from "react-native";
import {
  buildTerminalChrome,
  isLightTerminalTheme,
  resolveTerminalTheme,
  resolveTerminalThemeName,
} from "../../constants/terminalThemes";
import { useAppTheme } from "../../constants/tokens";
import { buildChatChrome } from "../../theme";

export function useTerminalThemeChrome() {
  const colorScheme = useColorScheme();
  const { theme: zenTheme } = useAppTheme();
  const themeName = useMemo(
    () => resolveTerminalThemeName(colorScheme),
    [colorScheme],
  );
  const terminalTheme = useMemo(
    () => resolveTerminalTheme(themeName),
    [themeName],
  );
  const chromeColors = useMemo(
    () => buildTerminalChrome(terminalTheme),
    [terminalTheme],
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
