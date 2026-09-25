import { useMemo } from "react";
import {
  buildTerminalChrome,
  type TerminalThemeChrome,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import { useAppTheme } from "../../constants/tokens";

/**
 * Git review chrome: the app's grouped canvas and content cards (so shared
 * EmptyState, InlineNotice and ActionMenu sit on matching material), while
 * change ink (green/red/cyan/yellow) keeps coming from the terminal palette.
 * `appBackground` is the grouped canvas, `surface` a content card or reader
 * page, `surfaceMuted` a quiet fill (gutter, hunk band, segmented track).
 */
export function useGitDiffChrome(theme: TerminalThemePalette): TerminalThemeChrome {
  const { colors, theme: appTheme } = useAppTheme();
  return useMemo(
    () => ({
      ...buildTerminalChrome(theme),
      appBackground: colors.bgPrimary,
      surface: colors.bgSurface,
      surfaceMuted: colors.surfaceSubtle,
      surfaceActive: colors.surfaceActive,
      composerInput: colors.inputBackground,
      border: appTheme.materials.separator,
      borderStrong: colors.borderStrong,
      text: colors.textPrimary,
      textMuted: colors.textSecondary,
      textSubtle: colors.textTertiary,
      textOnAccent: colors.textOnAccent,
      accent: colors.accent,
      accentSoft: colors.accentSoft,
      disabledSurface: colors.disabledSurface,
      focus: colors.focusRing,
      link: colors.accentStrong,
      danger: colors.dangerText,
      dangerSoft: colors.dangerSoft,
      overlay: colors.modalBackdrop,
      shadowColor: colors.shadowColor,
    }),
    [appTheme.materials.separator, colors, theme],
  );
}
