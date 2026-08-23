import type { AgentStatus } from '../constants/tokens';

export type AgentStatusIndicatorIcon =
  | 'checkmark-circle'
  | 'close-circle'
  | 'pause-circle'
  | 'help-circle-outline';

/** True only for durable active-turn Running from the daemon contract. */
export function isAgentActivelyRunning(status: AgentStatus): boolean {
  return status === 'running';
}

export function agentStatusLabel(status: AgentStatus): string {
  switch (status) {
    case 'failed':
      return 'Failed';
    case 'blocked':
      return 'Blocked';
    case 'running':
      return 'Running';
    case 'done':
      return 'Done';
    case 'unknown':
      // Backend unknown = no durable activity signal; list rows are live panes.
      return 'Idle';
    default:
      return 'Idle';
  }
}

export function agentStatusIndicatorIcon(
  status: AgentStatus,
): AgentStatusIndicatorIcon | null {
  switch (status) {
    case 'running':
      return null;
    case 'done':
      return 'checkmark-circle';
    case 'failed':
      return 'close-circle';
    case 'blocked':
      return 'pause-circle';
    case 'unknown':
      return 'help-circle-outline';
  }
}

export function buildAgentSessionAccessibilityLabel({
  title,
  status,
  preview,
  timeLabel,
  brainDelegated,
}: {
  title: string;
  status: AgentStatus;
  preview: string;
  timeLabel: string;
  brainDelegated: boolean;
}): string {
  return [
    title,
    brainDelegated ? 'Brain delegated' : null,
    agentStatusLabel(status),
    preview,
    isAgentActivelyRunning(status) ? null : timeLabel,
  ]
    .filter((part): part is string => Boolean(part))
    .join(', ');
}
