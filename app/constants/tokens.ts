import { Platform, type TextStyle, type ViewStyle } from "react-native";
import type { AppColors } from "../theme/palette";
import { useZenTheme } from "../theme/provider";
import type { MaterialPalette } from "../theme/types";

export type { AppColors } from "../theme/palette";
export type { MaterialPalette } from "../theme/types";

export type AppColorScheme = 'light' | 'dark';

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

export function useMaterials(): MaterialPalette {
  return useZenTheme().theme.materials;
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
  /** Grouped list sections and content cards. */
  card: 22,
  /** Bottom sheets and full-width floating panels. */
  sheet: 32,
  pill: 999,
} as const;

/** Minimum interactive target on both platforms (Apple HIG 44pt, Material 48dp). */
export const TouchTarget = Platform.OS === 'android' ? 48 : 44;

/**
 * Continuous (squircle) corners on iOS; Android ignores the key. Spread next
 * to any borderRadius on cards, sheets and capsules.
 */
export const ContinuousCorners: Pick<ViewStyle, 'borderCurve'> = {
  borderCurve: 'continuous',
};

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
  // UI mono typography (chat code blocks, previews). The terminal GRID has
  // its own denser typography in terminalFontDensity.ts: grid glyph size is
  // the PTY column budget and is deliberately smaller than readable UI copy.
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
  largeTitle: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 28,
    fontWeight: '500',
    lineHeight: 36,
    letterSpacing: 0,
  },
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

export type WorkerStatus = 'running' | 'blocked' | 'done' | 'failed' | 'unknown';

export function workerStatusColor(status: WorkerStatus, colors: AppColors): string {
  switch (status) {
    case 'failed':
      return colors.statusFailed;
    case 'blocked':
      return colors.statusBlocked;
    case 'running':
      return colors.statusRunning;
    case 'done':
      return colors.statusDone;
    default:
      return colors.statusUnknown;
  }
}

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

/**
 * Diffuse ambient elevation. `card` lifts content off the grouped canvas,
 * `raised` separates controls, `float` belongs to sheets, menus and FABs.
 */
export function shadow(
  level: 'card' | 'raised' | 'float',
  color = '#000000',
): ShadowStyle {
  switch (level) {
    case 'card':
      return makeShadow(color, 0.05, 12, 3, 1);
    case 'raised':
      return makeShadow(color, 0.09, 20, 8, 3);
    case 'float':
      return makeShadow(color, 0.16, 32, 16, 10);
  }
}
