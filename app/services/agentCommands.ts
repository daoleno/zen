const QUOTE_TRIM_RE = /^['"]|['"]$/g;

export const CLAUDE_CODE_COMMAND = "claude --dangerously-skip-permissions";
export const CODEX_COMMAND = "codex --approval-mode auto";

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

  return normalized === "codex" || normalized.startsWith("codex ") || commandBinary(normalized) === "codex";
}
