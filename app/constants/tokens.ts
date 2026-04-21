export const Colors = {
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
} as const;

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
