import type { Agent } from '../store/agents';
import { stripAnsiText } from './ansiText';

export type SessionPreviewTone = 'default' | 'muted' | 'accent' | 'danger' | 'success';

export type SessionPreview = {
  text: string;
  tone: SessionPreviewTone;
  prefix?: string;
};

export function formatAgentSessionPreview(
  agent: Pick<
    Agent,
    | 'status'
    | 'summary'
    | 'attention'
    | 'last_output_lines'
    | 'delegated'
    | 'needs_attention'
    | 'phase'
  >,
  options?: { showServerName?: boolean; serverName?: string },
): SessionPreview {
  const serverPrefix = options?.showServerName && options.serverName
    ? `${options.serverName}: `
    : undefined;
  const lastLine = extractLastOutputLine(agent.last_output_lines);
  const summary = agent.summary?.trim();

  if (agent.status === 'running') {
    if (agent.delegated) {
      return {
        text: lastLine || summary || 'No recent output',
        tone: 'accent',
        prefix: serverPrefix,
      };
    }
    if (lastLine) {
      return {
        text: lastLine,
        tone: 'default',
        prefix: serverPrefix,
      };
    }
    if (summary) {
      return {
        text: summary,
        tone: 'accent',
        prefix: serverPrefix,
      };
    }
    return {
      text: 'No recent output',
      tone: 'accent',
      prefix: serverPrefix,
    };
  }

  if (agent.status === 'blocked' || agent.status === 'failed') {
    const detail = agent.attention?.trim() || agent.phase?.trim();
    return {
      text: lastLine || summary || detail || 'No recent output',
      tone: 'danger',
      prefix: serverPrefix,
    };
  }

  if (agent.status === 'done') {
    return {
      text: lastLine || summary || 'No recent output',
      tone: 'muted',
      prefix: serverPrefix,
    };
  }

  if (agent.needs_attention) {
    return {
      text: agent.attention?.trim() || 'Waiting for input',
      tone: 'danger',
      prefix: serverPrefix,
    };
  }

  return {
    text: lastLine || summary || 'No recent output',
    tone: 'muted',
    prefix: serverPrefix,
  };
}

function extractLastOutputLine(lines: string[]): string {
  for (let index = lines.length - 1; index >= 0; index -= 1) {
    const cleaned = stripAnsiText(lines[index] ?? '');
    if (!cleaned || isNoiseLine(cleaned)) {
      continue;
    }
    return truncatePreview(cleaned);
  }
  return '';
}

function isNoiseLine(line: string): boolean {
  const normalized = line.trim();
  if (!normalized) {
    return true;
  }
  if (/^[\-_─═│┌┐└┘├┤┬┴┼╭╮╯╰╱╲╳\s]+$/.test(normalized)) {
    return true;
  }
  if (/^[\$>#%]\s*$/.test(normalized)) {
    return true;
  }
  return false;
}

function truncatePreview(value: string, max = 72): string {
  const compact = value.replace(/\s+/g, ' ').trim();
  if (compact.length <= max) {
    return compact;
  }
  return `${compact.slice(0, max - 1)}…`;
}
