import type { ConnectionState } from '../store/agents';
import { isClaudeCommand, isCodexCommand, isCursorAgentCommand, isGrokCommand, isOpenCodeCommand, isPiCommand } from './agentCommands';
import type { AgentKind } from './agentPresentation';

export function agentKindFromCommand(command?: string): AgentKind {
  if (isClaudeCommand(command)) return 'claude';
  if (isCodexCommand(command)) return 'codex';
  if (isCursorAgentCommand(command)) return 'cursor';
  if (isGrokCommand(command)) return 'grok';
  if (isPiCommand(command)) return 'pi';
  if (isOpenCodeCommand(command)) return 'opencode';
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
    case 'cursor':
      return 'Ask Cursor';
    case 'claude':
      return 'Ask Claude';
    default:
      return 'Message';
  }
}
