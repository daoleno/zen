import { describe, expect, test } from "bun:test";
import { resolveTheme } from "./resolve";
import {
  navigationThemeFromZenTheme,
  type NavigationThemeFonts,
} from "./navigation";

const fonts: NavigationThemeFonts = {
  regular: { fontFamily: "System", fontWeight: "400" },
  medium: { fontFamily: "System", fontWeight: "500" },
  bold: { fontFamily: "System", fontWeight: "600" },
  heavy: { fontFamily: "System", fontWeight: "700" },
};

describe("Session exit navigation theme continuity", () => {
  test("uses the resolved dark canvas for the native stack transition owner", () => {
    const zenTheme = resolveTheme({ colorScheme: "dark" });
    const navigationTheme = navigationThemeFromZenTheme(zenTheme, fonts);

    expect(navigationTheme).toEqual({
      dark: true,
      colors: {
        primary: zenTheme.colors.accent,
        background: zenTheme.colors.bgPrimary,
        card: zenTheme.colors.bgSurface,
        text: zenTheme.colors.textPrimary,
        border: zenTheme.colors.border,
        notification: zenTheme.colors.statusFailed,
      },
      fonts,
    });
    expect(navigationTheme.colors.background).toBe("#0F0F14");
    expect(Object.values(navigationTheme.colors)).not.toContain("transparent");
  });

  test("re-resolves the complete navigation theme for live Light/Dark changes", () => {
    const darkZenTheme = resolveTheme({ colorScheme: "dark" });
    const lightZenTheme = resolveTheme({ colorScheme: "light" });
    const darkNavigationTheme = navigationThemeFromZenTheme(
      darkZenTheme,
      fonts,
    );
    const lightNavigationTheme = navigationThemeFromZenTheme(
      lightZenTheme,
      fonts,
    );

    expect(darkNavigationTheme.dark).toBe(true);
    expect(lightNavigationTheme.dark).toBe(false);
    expect(darkNavigationTheme.colors.background).toBe(
      darkZenTheme.colors.bgPrimary,
    );
    expect(lightNavigationTheme.colors.background).toBe(
      lightZenTheme.colors.bgPrimary,
    );
    expect(lightNavigationTheme.colors.card).toBe(
      lightZenTheme.colors.bgSurface,
    );
    expect(lightNavigationTheme.colors.text).toBe(
      lightZenTheme.colors.textPrimary,
    );
    expect(darkNavigationTheme.fonts).toBe(fonts);
    expect(lightNavigationTheme.fonts).toBe(fonts);
  });

  test("follows the resolved theme instead of maintaining a second scheme", () => {
    const explicitlyDarkZenTheme = resolveTheme({
      colorScheme: "light",
      themeId: "classic-dark",
    });
    const navigationTheme = navigationThemeFromZenTheme(
      explicitlyDarkZenTheme,
      fonts,
    );

    expect(explicitlyDarkZenTheme.colorScheme).toBe("dark");
    expect(navigationTheme.dark).toBe(true);
    expect(navigationTheme.colors.background).toBe("#0F0F14");
  });
});
