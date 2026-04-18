const QUOTE_TRIM_RE = /^['"]|['"]$/g;

export const CLAUDE_CODE_COMMAND = "claude --dangerously-skip-permissions";
export const CODEX_COMMAND = "codex --dangerously-bypass-approvals-and-sandbox";

export type SupportedAgentID = "claude" | "codex";

export interface SupportedAgentTarget {
  id: SupportedAgentID;
  handle: string;
  label: string;
  command: string;
  description: string;
}

export const SUPPORTED_AGENT_TARGETS: SupportedAgentTarget[] = [
  {
    id: "claude",
    handle: "claude",
    label: "Claude Code",
    command: CLAUDE_CODE_COMMAND,
    description: "Autonomous Claude Code run in this issue worktree.",
  },
  {
    id: "codex",
    handle: "codex",
    label: "Codex",
    command: CODEX_COMMAND,
    description: "Autonomous Codex run in this issue worktree.",
  },
];

function normalizeCommand(command?: string) {
  return command?.trim().toLowerCase() || "";
}

function commandBinary(command?: string) {
  const normalized = normalizeCommand(command);
  if (!normalized) {
    return "";
  }

  const token = normalized.split(/\s+/, 1)[0]?.replace(QUOTE_TRIM_RE, "") || "";
  const parts = token.split("/");
  return parts[parts.length - 1] || token;
}

export function isClaudeCommand(command?: string) {
  const normalized = normalizeCommand(command);
  if (!normalized) {
    return false;
  }

  return (
    normalized === "claude" ||
    normalized === "claude code" ||
    normalized.startsWith("claude ") ||
    commandBinary(normalized) === "claude"
  );
}

export function isCodexCommand(command?: string) {
  const normalized = normalizeCommand(command);
  if (!normalized) {
    return false;
  }

  return (
    normalized === "codex" ||
    normalized.startsWith("codex ") ||
    commandBinary(normalized) === "codex"
  );
}

export function findSupportedAgentByHandle(handle?: string) {
  const normalized = handle?.trim().toLowerCase() || "";
  if (!normalized) {
    return null;
  }

  return (
    SUPPORTED_AGENT_TARGETS.find((target) => target.handle === normalized) ||
    null
  );
}

export function findSupportedAgentMention(text?: string) {
  const source = text || "";
  for (const target of SUPPORTED_AGENT_TARGETS) {
    const pattern = new RegExp(`(^|\\s)@${target.handle}(?=\\s|$)`, "i");
    if (pattern.test(source)) {
      return target;
    }
  }
  return null;
}

export function stripSupportedAgentMentions(text?: string) {
  let next = text || "";

  for (const target of SUPPORTED_AGENT_TARGETS) {
    const pattern = new RegExp(`(^|\\s)@${target.handle}(?=\\s|$)`, "gi");
    next = next.replace(pattern, "$1");
  }

  return next
    .replace(/[ \t]+\n/g, "\n")
    .replace(/\n[ \t]+/g, "\n")
    .replace(/[ \t]{2,}/g, " ")
    .trim();
}
