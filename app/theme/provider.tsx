import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { useColorScheme } from 'react-native';
import { getThemePreference, setThemePreference } from '../services/storage';
import { resolveTheme } from './resolve';
import type { ResolvedZenTheme, ThemePreference } from './types';

type ThemeContextValue = {
  theme: ResolvedZenTheme;
  preference: ThemePreference;
  setPreference: (next: ThemePreference) => Promise<void>;
};

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const systemScheme = useColorScheme();
  const [preference, setPreferenceState] = useState<ThemePreference>('system');
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

  const colorScheme = systemScheme === 'light' ? 'light' : 'dark';
  const themeId = preference === 'system' ? null : preference;

  const theme = useMemo(
    () =>
      resolveTheme({
        colorScheme,
        themeId,
      }),
    [colorScheme, themeId],
  );

  const setPreference = useCallback(async (next: ThemePreference) => {
    setPreferenceState(next);
    await setThemePreference(next);
  }, []);

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
    <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
  );
}

export function useZenTheme(): ThemeContextValue {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error('useZenTheme must be used within ThemeProvider');
  }
  return context;
}
