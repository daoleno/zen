import { useColorScheme } from "react-native";

export interface AppColors {
  bgPrimary: string;
  bgSurface: string;
  bgElevated: string;
  textPrimary: string;
  textSecondary: string;
  accent: string;
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
}

export const DarkColors: AppColors = {
  bgPrimary: '#0F0F14',
  bgSurface: '#1A1A24',
  bgElevated: '#242434',
  textPrimary: '#E8E8ED',
  textSecondary: '#8888A0',
  accent: '#5B9DFF',
  statusFailed: '#FF5252',
  statusBlocked: '#FF5252',
  statusUnknown: '#FFB74D',
  statusRunning: '#4CAF50',
  statusDone: '#666680',
  zenGreen: '#2E7D32',
  priorityUrgent: '#FF5252',
  priorityHigh: '#FF9500',
  priorityMedium: '#FFB74D',
  priorityLow: '#8888A0',
  border: 'rgba(255,255,255,0.08)',
  borderSubtle: 'rgba(255,255,255,0.05)',
  borderStrong: 'rgba(255,255,255,0.12)',
  surfaceSubtle: 'rgba(255,255,255,0.03)',
  surfacePressed: 'rgba(255,255,255,0.05)',
  surfaceActive: 'rgba(255,255,255,0.08)',
  inputBackground: 'rgba(255,255,255,0.045)',
  modalBackdrop: 'rgba(0,0,0,0.66)',
  modalSurface: '#15151C',
  modalSurfaceAlt: '#1A1A22',
  textOnAccent: '#0F0F14',
  promptGreen: '#8FB573',
  promptYellow: '#E6B450',
  warning: '#E7B65C',
  dangerText: '#F09999',
  disabledText: '#65758A',
};

export const LightColors: AppColors = {
  bgPrimary: '#F7F8FB',
  bgSurface: '#FFFFFF',
  bgElevated: '#E8EEF6',
  textPrimary: '#151922',
  textSecondary: '#667085',
  accent: '#2563EB',
  statusFailed: '#D92D20',
  statusBlocked: '#D92D20',
  statusUnknown: '#B7791F',
  statusRunning: '#16803A',
  statusDone: '#8A94A6',
  zenGreen: '#2E7D32',
  priorityUrgent: '#D92D20',
  priorityHigh: '#D97706',
  priorityMedium: '#B7791F',
  priorityLow: '#667085',
  border: 'rgba(21,25,34,0.10)',
  borderSubtle: 'rgba(21,25,34,0.07)',
  borderStrong: 'rgba(21,25,34,0.16)',
  surfaceSubtle: 'rgba(21,25,34,0.035)',
  surfacePressed: 'rgba(21,25,34,0.06)',
  surfaceActive: 'rgba(37,99,235,0.10)',
  inputBackground: 'rgba(21,25,34,0.045)',
  modalBackdrop: 'rgba(15,23,42,0.34)',
  modalSurface: '#FFFFFF',
  modalSurfaceAlt: '#F2F5F9',
  textOnAccent: '#FFFFFF',
  promptGreen: '#3F7C50',
  promptYellow: '#9A6B1F',
  warning: '#A36A00',
  dangerText: '#C24141',
  disabledText: '#94A3B8',
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
  sm: 8,
  md: 12,
  lg: 16,
  pill: 999,
} as const;

export const Typography = {
  uiFont: 'SourceHanSansSC-Regular',
  uiFontMedium: 'SourceHanSansSC-Medium',
  terminalFont: 'MapleMono-CN-Regular',
  terminalFontBold: 'MapleMono-CN-SemiBold',
  terminalSize: 13,
  agentNameSize: 15,
  statusTextSize: 13,
  metadataSize: 11,
} as const;

export type AgentStatus = 'running' | 'blocked' | 'done' | 'failed' | 'unknown';
export type RunStatus = 'queued' | 'running' | 'blocked' | 'done' | 'failed' | 'cancelled';

export type IssueStatus = 'backlog' | 'todo' | 'in_progress' | 'done' | 'cancelled';
export type IssuePriority = 0 | 1 | 2 | 3 | 4;

export const statusColor = (status: AgentStatus): string => {
  switch (status) {
    case 'failed': return Colors.statusFailed;
    case 'blocked': return Colors.statusBlocked;
    case 'unknown': return Colors.statusUnknown;
    case 'running': return Colors.statusRunning;
    case 'done': return Colors.statusDone;
  }
};

export const issueStatusColor = (status: IssueStatus): string => {
  switch (status) {
    case 'in_progress': return Colors.statusRunning;
    case 'todo': return Colors.accent;
    case 'backlog': return Colors.textSecondary;
    case 'done': return Colors.statusDone;
    case 'cancelled': return Colors.statusDone;
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

export const priorityColor = (priority: IssuePriority): string => {
  switch (priority) {
    case 1: return Colors.priorityUrgent;
    case 2: return Colors.priorityHigh;
    case 3: return Colors.priorityMedium;
    case 4: return Colors.priorityLow;
    default: return 'transparent';
  }
};
