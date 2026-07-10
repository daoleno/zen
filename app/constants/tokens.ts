import { Platform, useColorScheme, type TextStyle, type ViewStyle } from "react-native";
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

/** Prevents Latin descenders (g, y, p) from clipping with Source Han Sans. */
export const UiTextMetrics: Pick<
  TextStyle,
  'includeFontPadding' | 'textAlignVertical'
> = Platform.select({
  android: {
    includeFontPadding: false,
    textAlignVertical: 'center',
  },
  default: {
    includeFontPadding: false,
  },
}) ?? { includeFontPadding: false };

/** Line height tuned for mixed Latin + CJK UI copy. */
export function uiLineHeight(fontSize: number): number {
  return Math.ceil(fontSize * 1.48);
}

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

type TypeScaleStyle = Pick<
  TextStyle,
  'fontFamily' | 'fontSize' | 'fontWeight' | 'letterSpacing' | 'lineHeight'
>;

/** Authoritative product type roles with explicit mixed Latin/CJK metrics. */
export const TypeScale = {
  display: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 30,
    fontWeight: '500',
    lineHeight: 38,
    letterSpacing: 0,
  },
  title: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 20,
    fontWeight: '500',
    lineHeight: 28,
    letterSpacing: 0,
  },
  heading: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 17,
    fontWeight: '500',
    lineHeight: 24,
    letterSpacing: 0,
  },
  body: {
    fontFamily: Typography.uiFont,
    fontSize: 15,
    fontWeight: '400',
    lineHeight: 23,
    letterSpacing: 0,
  },
  compact: {
    fontFamily: Typography.uiFont,
    fontSize: 14,
    fontWeight: '400',
    lineHeight: 21,
    letterSpacing: 0,
  },
  label: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 13,
    fontWeight: '500',
    lineHeight: 18,
    letterSpacing: 0,
  },
  caption: {
    fontFamily: Typography.uiFont,
    fontSize: 12,
    fontWeight: '400',
    lineHeight: 17,
    letterSpacing: 0,
  },
  micro: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 11,
    fontWeight: '500',
    lineHeight: 15,
    letterSpacing: 0,
  },
  mono: {
    fontFamily: Typography.terminalFont,
    fontSize: 13,
    fontWeight: '400',
    lineHeight: 20,
    letterSpacing: 0,
  },
  monoStrong: {
    fontFamily: Typography.terminalFontBold,
    fontSize: 13,
    fontWeight: '600',
    lineHeight: 20,
    letterSpacing: 0,
  },
} as const satisfies Record<string, TypeScaleStyle>;

export type TypeScaleRole = keyof typeof TypeScale;

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
