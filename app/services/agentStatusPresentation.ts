import type { AgentStatus } from '../constants/tokens';

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
