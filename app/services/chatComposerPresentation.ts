import type { ConnectionState } from '../store/agents';
import { isClaudeCommand, isCodexCommand, isGrokCommand } from './agentCommands';
import type { AgentKind } from './agentPresentation';

export function agentKindFromCommand(command?: string): AgentKind {
  if (isClaudeCommand(command)) return 'claude';
  if (isCodexCommand(command)) return 'codex';
  if (isGrokCommand(command)) return 'grok';
  return 'terminal';
}

export function chatAgentSupportsSlashCommands(kind: AgentKind): boolean {
  return kind === 'codex';
}

export function buildChatComposerPlaceholder({
  agentKind,
  connectionState,
  slashQueryActive,
  explicitPlaceholder,
}: {
  agentKind: AgentKind;
  connectionState: ConnectionState;
  slashQueryActive: boolean;
  explicitPlaceholder?: string;
}): string {
  if (explicitPlaceholder?.trim()) {
    return explicitPlaceholder.trim();
  }
  if (connectionState !== 'connected') {
    return connectionState === 'connecting'
      ? 'Connecting…'
      : 'Connection unavailable';
  }
  if (slashQueryActive && chatAgentSupportsSlashCommands(agentKind)) {
    return 'Search commands';
  }
  switch (agentKind) {
    case 'codex':
      return 'Ask Codex';
    case 'grok':
      return 'Ask Grok';
    case 'claude':
      return 'Ask Claude';
    default:
      return 'Message';
  }
}