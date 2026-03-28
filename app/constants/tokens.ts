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
} as const;

export const Spacing = {
  base: 8,
  rowHeight: 64,
  rowPaddingH: 16,
  rowPaddingV: 12,
  screenMargin: 16,
  actionBarHeight: 56,
} as const;

export const Typography = {
  terminalFont: 'monospace',
  terminalSize: 13,
  agentNameSize: 15,
  statusTextSize: 13,
  metadataSize: 11,
} as const;

export type AgentStatus = 'running' | 'blocked' | 'done' | 'failed' | 'unknown';

export const statusColor = (status: AgentStatus): string => {
  switch (status) {
    case 'failed': return Colors.statusFailed;
    case 'blocked': return Colors.statusBlocked;
    case 'unknown': return Colors.statusUnknown;
    case 'running': return Colors.statusRunning;
    case 'done': return Colors.statusDone;
  }
};
