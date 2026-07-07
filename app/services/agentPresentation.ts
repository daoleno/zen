import type { Agent } from '../store/agents';
import { displayPathSubtitle } from './pathDisplay';
import { isClaudeCommand, isCodexCommand, isCursorAgentCommand, isGrokCommand } from './agentCommands';
import {
  detectTerminalFlavor,
  terminalFlavorLabel,
  type TerminalFlavor,
} from './terminalFlavor';

export type AgentKind = 'terminal' | 'claude' | 'codex' | 'cursor' | 'grok';
export type AgentTitleSource = 'alias' | 'explicit_name' | 'default';
export type { TerminalFlavor };

export type PresentedAgent = {
  kind: AgentKind;
  terminalFlavor: TerminalFlavor;
  title: string;
  shortTitle: string;
  subtitle: string;
  typeLabel: string;
  cwdBase: string;
  titleSource: AgentTitleSource;
};

export function presentAgent(agent: Pick<Agent, 'name' | 'project' | 'cwd' | 'command' | 'summary' | 'last_output_lines'>, alias?: string): PresentedAgent {
  const kind = detectAgentKind(agent);
  const terminalFlavor =
    kind === 'terminal' ? detectTerminalFlavor(agent) : 'shell';
  const label = typeLabel(kind, terminalFlavor);
  const cwd = normalize(agent.cwd);
  const cwdBase = basename(cwd);
  const project = normalize(agent.project);
  const cleanName = sanitizeName(agent.name);
  const explicitAlias = normalize(alias);
  const location = project || cwdBase;
  const fallbackTitle = location || defaultTitle(kind);

  if (explicitAlias) {
    return {
      kind,
      terminalFlavor,
      title: explicitAlias,
      shortTitle: explicitAlias,
      subtitle: buildSubtitle(label, cwd || project),
      typeLabel: label,
      cwdBase,
      titleSource: 'alias',
    };
  }

  const hasAgentTitle = cleanName && !isGenericAgentTitle(cleanName, kind);
  const title = hasAgentTitle ? cleanName : fallbackTitle;

  return {
    kind,
    terminalFlavor,
    title,
    shortTitle: hasAgentTitle ? title : (location || shortDefaultTitle(kind)),
    subtitle: buildSubtitle(label, location || cwd),
    typeLabel: label,
    cwdBase,
    titleSource: hasAgentTitle ? 'explicit_name' : 'default',
  };
}

function detectAgentKind(agent: Pick<Agent, 'name' | 'project' | 'cwd' | 'command' | 'summary' | 'last_output_lines'>): AgentKind {
  if (isClaudeCommand(agent.command)) return 'claude';
  if (isCodexCommand(agent.command)) return 'codex';
  if (isCursorAgentCommand(agent.command)) return 'cursor';
  if (isGrokCommand(agent.command)) return 'grok';
  return 'terminal';
}

function isGenericAgentTitle(name: string, kind: AgentKind): boolean {
  if (!name) return true;
  const lower = name.toLowerCase().replace(/\s+/g, ' ').trim();
  if (
    kind === 'claude' && (
      lower === 'claude' ||
      lower === 'claude code' ||
      lower === 'claude-code'
    )
  ) return true;
  if (kind === 'codex' && (lower === 'codex' || lower === 'openai codex')) return true;
  if (kind === 'cursor' && (lower === 'agent' || lower === 'cursor' || lower === 'cursor agent')) return true;
  if (kind === 'grok' && (lower === 'grok' || lower === 'grok cli' || lower === 'xai grok')) return true;
  if (
    lower === 'zsh' ||
    lower === 'bash' ||
    lower === 'sh' ||
    lower === 'fish' ||
    lower === 'shell' ||
    lower === 'terminal' ||
    lower === 'tmux' ||
    lower === '[tmux]' ||
    lower === 'node' ||
    lower === 'bun' ||
    lower === 'python' ||
    lower === 'python3' ||
    lower.includes('tmux') ||
    lower.startsWith('./') ||
    lower.startsWith('/')
  ) return true;
  return /^[\w.-]+:[@%\w.-]+$/.test(lower);
}

function sanitizeName(value?: string): string {
  const trimmed = normalize(value);
  if (!trimmed) return '';
  return trimmed.replace(/\s+\([^)]+\)\s*$/, '').trim();
}

function basename(value: string): string {
  if (!value) return '';
  const normalized = value.replace(/\/+$/, '');
  const parts = normalized.split('/');
  return parts[parts.length - 1] || normalized;
}

function normalize(value?: string): string {
  return value?.trim() || '';
}

function defaultTitle(kind: AgentKind): string {
  switch (kind) {
    case 'claude':
      return 'Claude';
    case 'codex':
      return 'Codex';
    case 'cursor':
      return 'Cursor Agent';
    case 'grok':
      return 'Grok';
    default:
      return 'Shell';
  }
}

function shortDefaultTitle(kind: AgentKind): string {
  switch (kind) {
    case 'claude':
      return 'Claude';
    case 'codex':
      return 'Codex';
    case 'cursor':
      return 'Cursor';
    case 'grok':
      return 'Grok';
    default:
      return 'Shell';
  }
}

function typeLabel(kind: AgentKind, terminalFlavor: TerminalFlavor): string {
  switch (kind) {
    case 'claude':
      return 'Claude Code';
    case 'codex':
      return 'OpenAI Codex';
    case 'cursor':
      return 'Cursor Agent';
    case 'grok':
      return 'Grok';
    default:
      return terminalFlavorLabel(terminalFlavor);
  }
}

function buildSubtitle(label: string, location: string): string {
  const compactLocation = displayPathSubtitle(location);
  return [label, compactLocation].filter(Boolean).join(' · ');
}
