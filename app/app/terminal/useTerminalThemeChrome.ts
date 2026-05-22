import { useMemo } from "react";
import { useColorScheme } from "react-native";
import {
  buildTerminalChrome,
  isLightTerminalTheme,
  resolveTerminalTheme,
  resolveTerminalThemePreference,
  type TerminalThemePreference,
} from "../../constants/terminalThemes";

export function useTerminalThemeChrome(
  themePreference: TerminalThemePreference,
) {
  const colorScheme = useColorScheme();
  const themeName = useMemo(
    () => resolveTerminalThemePreference(themePreference, colorScheme),
    [themePreference, colorScheme],
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
    themeName,
  };
}
