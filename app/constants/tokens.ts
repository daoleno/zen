import { Platform, useColorScheme, type ViewStyle } from "react-native";
import { classicDarkTheme } from "../theme/definitions/classicDark";
import { classicLightTheme } from "../theme/definitions/classicLight";
import type { AppColors } from "../theme/palette";
import { useZenTheme } from "../theme/provider";
import { resolveTheme } from "../theme/resolve";

export type { AppColors } from "../theme/palette";

/** @deprecated Use useAppColors() or classicDarkTheme.colors. */
export const DarkColors: AppColors = classicDarkTheme.colors;

/** @deprecated Use useAppColors() or classicLightTheme.colors. */
export const LightColors: AppColors = classicLightTheme.colors;

export const Colors = DarkColors;

export type AppColorScheme = 'light' | 'dark';

export function colorsForScheme(
  scheme: ReturnType<typeof useColorScheme>,
): AppColors {
  return resolveTheme({
    colorScheme: scheme === 'light' ? 'light' : 'dark',
  }).colors;
}

export function useAppTheme(): {
  colors: AppColors;
  colorScheme: AppColorScheme;
  isLight: boolean;
  theme: ReturnType<typeof useZenTheme>['theme'];
} {
  const { theme } = useZenTheme();
  return {
    colors: theme.colors,
    colorScheme: theme.colorScheme,
    isLight: theme.isLight,
    theme,
  };
}

export function useAppColors(): AppColors {
  return useZenTheme().theme.colors;
}

export const Spacing = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 24,
  xxl: 32,
  base: 8,
  rowHeight: 64,
  rowPaddingH: 16,
  rowPaddingV: 12,
  screenMargin: 16,
  actionBarHeight: 56,
} as const;

export const Radii = {
  xs: 8,
  sm: 12,
  md: 16,
  lg: 20,
  xl: 24,
  xxl: 28,
  pill: 999,
} as const;

export const Typography = {
  uiFont: 'SourceHanSansSC-Regular',
  uiFontMedium: 'SourceHanSansSC-Medium',
  terminalFont: 'MapleMono-CN-Regular',
  terminalFontBold: 'MapleMono-CN-SemiBold',
  chatFont: 'SourceHanSansSC-Regular',
  chatFontMedium: 'SourceHanSansSC-Medium',
  chatMonoFont: 'MapleMono-CN-Regular',
  chatMonoFontBold: 'MapleMono-CN-SemiBold',
  terminalSize: 13,
  agentNameSize: 15,
  statusTextSize: 13,
  metadataSize: 11,
} as const;

export type AgentStatus = 'running' | 'blocked' | 'done' | 'failed' | 'unknown';
export type RunStatus = 'queued' | 'running' | 'blocked' | 'done' | 'failed' | 'cancelled';

export const statusColor = (status: AgentStatus): string => {
  switch (status) {
    case 'failed': return Colors.statusFailed;
    case 'blocked': return Colors.statusBlocked;
    case 'unknown': return Colors.statusUnknown;
    case 'running': return Colors.statusRunning;
    case 'done': return Colors.statusDone;
  }
};

export const runStatusColor = (status: RunStatus | string): string => {
  switch (status) {
    case 'queued': return Colors.statusUnknown;
    case 'running': return Colors.statusRunning;
    case 'blocked': return Colors.statusBlocked;
    case 'failed': return Colors.statusFailed;
    case 'done': return Colors.statusDone;
    case 'cancelled': return Colors.textSecondary;
    default: return Colors.textSecondary;
  }
};

type ShadowStyle = Pick<
  ViewStyle,
  'shadowColor' | 'shadowOffset' | 'shadowOpacity' | 'shadowRadius' | 'elevation'
>;

function makeShadow(
  color: string,
  opacity: number,
  radius: number,
  height: number,
  elevation: number,
): ShadowStyle {
  return Platform.select<ShadowStyle>({
    ios: {
      shadowColor: color,
      shadowOffset: { width: 0, height },
      shadowOpacity: opacity,
      shadowRadius: radius,
    },
    default: {
      shadowColor: color,
      elevation,
    },
  }) as ShadowStyle;
}

/** Soft, tactile elevation — the gentle lift that makes playful UI feel friendly. */
export function shadow(
  level: 'card' | 'raised' | 'float',
  color = '#000000',
): ShadowStyle {
  switch (level) {
    case 'card':
      return makeShadow(color, 0.06, 10, 4, 2);
    case 'raised':
      return makeShadow(color, 0.10, 18, 8, 4);
    case 'float':
      return makeShadow(color, 0.18, 28, 14, 8);
  }
}
