import { Platform, useColorScheme, type ViewStyle } from "react-native";

export interface AppColors {
  bgPrimary: string;
  bgSurface: string;
  bgElevated: string;
  textPrimary: string;
  textSecondary: string;
  textTertiary: string;
  accent: string;
  accentSoft: string;
  accentStrong: string;
  statusFailed: string;
  statusBlocked: string;
  statusUnknown: string;
  statusRunning: string;
  statusDone: string;
  zenGreen: string;
  priorityUrgent: string;
  priorityHigh: string;
  priorityMedium: string;
  priorityLow: string;
  border: string;
  borderSubtle: string;
  borderStrong: string;
  surfaceSubtle: string;
  surfacePressed: string;
  surfaceActive: string;
  inputBackground: string;
  modalBackdrop: string;
  modalSurface: string;
  modalSurfaceAlt: string;
  textOnAccent: string;
  promptGreen: string;
  promptYellow: string;
  warning: string;
  dangerText: string;
  disabledText: string;
  dangerSoft: string;
  warningSoft: string;
  shadowColor: string;
}

// "Slate & Sky" — clean cool neutrals with a fresh sky-blue accent. Playful
// structure (rounded, soft) but a crisper, less muddy palette than warm clay.
export const DarkColors: AppColors = {
  bgPrimary: '#0E1116',
  bgSurface: '#161A22',
  bgElevated: '#1E232E',
  textPrimary: '#F2F4F8',
  textSecondary: '#8B95A7',
  textTertiary: '#525C6E',
  accent: '#4C8DFF',
  accentSoft: 'rgba(76,141,255,0.16)',
  accentStrong: '#6BA0FF',
  statusFailed: '#FF6B81',
  statusBlocked: '#FF6B81',
  statusUnknown: '#F2B441',
  statusRunning: '#3DD682',
  statusDone: '#525C6E',
  zenGreen: '#3DD682',
  priorityUrgent: '#FF6B81',
  priorityHigh: '#FF9F45',
  priorityMedium: '#F2B441',
  priorityLow: '#8B95A7',
  border: 'rgba(242,244,248,0.09)',
  borderSubtle: 'rgba(242,244,248,0.05)',
  borderStrong: 'rgba(242,244,248,0.15)',
  surfaceSubtle: 'rgba(242,244,248,0.045)',
  surfacePressed: 'rgba(242,244,248,0.07)',
  surfaceActive: 'rgba(76,141,255,0.18)',
  inputBackground: 'rgba(242,244,248,0.06)',
  modalBackdrop: 'rgba(0,0,0,0.62)',
  modalSurface: '#181C24',
  modalSurfaceAlt: '#1E232E',
  textOnAccent: '#0E1116',
  promptGreen: '#8FB573',
  promptYellow: '#E6B450',
  warning: '#F2B441',
  dangerText: '#FF8A9B',
  disabledText: '#525C6E',
  dangerSoft: 'rgba(255,107,129,0.14)',
  warningSoft: 'rgba(242,180,65,0.14)',
  shadowColor: '#000000',
};

export const LightColors: AppColors = {
  bgPrimary: '#F6F8FB',
  bgSurface: '#FFFFFF',
  bgElevated: '#EEF2F8',
  textPrimary: '#14181F',
  textSecondary: '#5A6577',
  textTertiary: '#9AA4B5',
  accent: '#2E7CFF',
  accentSoft: 'rgba(46,124,255,0.10)',
  accentStrong: '#1E6AEF',
  statusFailed: '#E0414E',
  statusBlocked: '#E0414E',
  statusUnknown: '#C99522',
  statusRunning: '#1FA861',
  statusDone: '#9AA4B5',
  zenGreen: '#1FA861',
  priorityUrgent: '#E0414E',
  priorityHigh: '#D98424',
  priorityMedium: '#C99522',
  priorityLow: '#5A6577',
  border: 'rgba(20,24,31,0.10)',
  borderSubtle: 'rgba(20,24,31,0.06)',
  borderStrong: 'rgba(20,24,31,0.16)',
  surfaceSubtle: 'rgba(20,24,31,0.04)',
  surfacePressed: 'rgba(20,24,31,0.06)',
  surfaceActive: 'rgba(46,124,255,0.12)',
  inputBackground: 'rgba(20,24,31,0.05)',
  modalBackdrop: 'rgba(20,24,31,0.36)',
  modalSurface: '#FFFFFF',
  modalSurfaceAlt: '#EEF2F8',
  textOnAccent: '#FFFFFF',
  promptGreen: '#3F7C50',
  promptYellow: '#9A6B1F',
  warning: '#C99522',
  dangerText: '#C8323F',
  disabledText: '#9AA4B5',
  dangerSoft: 'rgba(224,65,78,0.10)',
  warningSoft: 'rgba(201,149,34,0.12)',
  shadowColor: '#243044',
};

export const Colors = DarkColors;

export type AppColorScheme = 'light' | 'dark';

export function colorsForScheme(
  scheme: ReturnType<typeof useColorScheme>,
): AppColors {
  return scheme === 'light' ? LightColors : DarkColors;
}

export function useAppTheme(): {
  colors: AppColors;
  colorScheme: AppColorScheme;
  isLight: boolean;
} {
  const scheme = useColorScheme();
  const isLight = scheme === 'light';
  return {
    colors: isLight ? LightColors : DarkColors,
    colorScheme: isLight ? 'light' : 'dark',
    isLight,
  };
}

export function useAppColors(): AppColors {
  return useAppTheme().colors;
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
