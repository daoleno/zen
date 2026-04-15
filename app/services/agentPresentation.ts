import type { Agent } from '../store/agents';
import { isClaudeCommand, isCodexCommand } from './agentCommands';

export type AgentKind = 'terminal' | 'claude' | 'codex';

export type PresentedAgent = {
  kind: AgentKind;
  title: string;
  shortTitle: string;
  subtitle: string;
  cwdBase: string;
};

export function presentAgent(agent: Pick<Agent, 'name' | 'project' | 'cwd' | 'command' | 'summary' | 'last_output_lines'>, alias?: string): PresentedAgent {
  const kind = detectAgentKind(agent);
  const cwd = normalize(agent.cwd);
  const cwdBase = basename(cwd);
  const project = normalize(agent.project);
  const cleanName = sanitizeName(agent.name);
  const explicitAlias = normalize(alias);
  const location = project || cwdBase;
  const generatedTitle = defaultTitle(kind);

  if (explicitAlias) {
    return {
      kind,
      title: explicitAlias,
      shortTitle: explicitAlias,
      subtitle: buildSubtitle(kind, cwd || project),
      cwdBase,
    };
  }

  const title = shouldPreferGeneratedTitle(cleanName, kind) ? generatedTitle : (cleanName || generatedTitle);

  return {
    kind,
    title,
    shortTitle: title,
    subtitle: buildSubtitle(kind, location || cwd),
    cwdBase,
  };
}

function detectAgentKind(agent: Pick<Agent, 'name' | 'project' | 'cwd' | 'command' | 'summary' | 'last_output_lines'>): AgentKind {
  if (isClaudeCommand(agent.command)) return 'claude';
  if (isCodexCommand(agent.command)) return 'codex';
  return 'terminal';
}

function shouldPreferGeneratedTitle(name: string, kind: AgentKind): boolean {
  if (!name) return true;
  const lower = name.toLowerCase();
  if (kind === 'claude' && (lower === 'claude' || lower === 'claude code')) return true;
  if (kind === 'codex' && lower === 'codex') return true;
  if (
    kind === 'terminal' && (
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
    )
  ) return true;
  return /\(\w+:\d+\)\s*$/.test(name);
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
      return 'Claude Code';
    case 'codex':
      return 'Codex';
    default:
      return 'Terminal';
  }
}

function typeLabel(kind: AgentKind): string {
  switch (kind) {
    case 'claude':
      return 'Claude Code';
    case 'codex':
      return 'OpenAI Codex';
    default:
      return 'Shell terminal';
  }
}

function buildSubtitle(kind: AgentKind, location: string): string {
  return [typeLabel(kind), location].filter(Boolean).join(' · ');
}
