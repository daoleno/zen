import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import {
  DefaultTheme,
  ThemeProvider as NavigationThemeProvider,
} from "expo-router";
import * as SystemUI from "expo-system-ui";
import { useColorScheme } from "react-native";
import { getThemePreference, setThemePreference } from "../services/storage";
import { navigationThemeFromZenTheme } from "./navigation";
import { resolveTheme } from "./resolve";
import { syncSystemRootBackground } from "./syncSystemRootBackground";
import type { ResolvedZenTheme, ThemePreference } from "./types";

type ThemeContextValue = {
  theme: ResolvedZenTheme;
  preference: ThemePreference;
  setPreference: (next: ThemePreference) => Promise<void>;
};

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const systemScheme = useColorScheme();
  const [preference, setPreferenceState] = useState<ThemePreference>("system");
  const [hydrated, setHydrated] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const stored = await getThemePreference().catch(() => null);
      if (cancelled) return;
      if (stored) {
        setPreferenceState(stored);
      }
      setHydrated(true);
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const colorScheme = systemScheme === "light" ? "light" : "dark";
  const themeId = preference === "system" ? null : preference;

  const theme = useMemo(
    () =>
      resolveTheme({
        colorScheme,
        themeId,
      }),
    [colorScheme, themeId],
  );
  const navigationTheme = useMemo(
    () => navigationThemeFromZenTheme(theme, DefaultTheme.fonts),
    [theme],
  );

  const setPreference = useCallback(async (next: ThemePreference) => {
    setPreferenceState(next);
    await setThemePreference(next);
  }, []);

  useEffect(() => {
    if (!hydrated) {
      return;
    }
    // Expo system root follows resolved Zen canvas on Android and iOS.
    // Best-effort; ThemeProvider remains the sole theme state owner.
    void syncSystemRootBackground(theme.colors.bgPrimary, {
      setBackgroundColorAsync: (color) =>
        SystemUI.setBackgroundColorAsync(color),
    });
  }, [hydrated, theme.colors.bgPrimary]);

  const value = useMemo(
    () => ({
      theme,
      preference,
      setPreference,
    }),
    [preference, setPreference, theme],
  );

  if (!hydrated) {
    return null;
  }

  return (
    <ThemeContext.Provider value={value}>
      <NavigationThemeProvider value={navigationTheme}>
        {children}
      </NavigationThemeProvider>
    </ThemeContext.Provider>
  );
}

export function useZenTheme(): ThemeContextValue {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error("useZenTheme must be used within ThemeProvider");
  }
  return context;
}
