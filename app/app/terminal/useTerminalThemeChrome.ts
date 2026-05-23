import { useMemo } from "react";
import { useColorScheme } from "react-native";
import {
  buildTerminalChrome,
  isLightTerminalTheme,
  resolveTerminalTheme,
  resolveTerminalThemeName,
} from "../../constants/terminalThemes";

export function useTerminalThemeChrome() {
  const colorScheme = useColorScheme();
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
  const statusBarStyle: "dark" | "light" = isLightTerminalTheme(terminalTheme)
    ? "dark"
    : "light";

  return {
    chromeColors,
    statusBarStyle,
    terminalTheme,
  };
}
