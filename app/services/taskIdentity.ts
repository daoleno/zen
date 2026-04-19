const DEFAULT_ISSUE_PREFIX = "ZEN";

const PROJECT_KEY_IGNORED_TRAILING_TOKENS = new Set([
  "API",
  "APP",
  "BACKEND",
  "CLI",
  "CLIENT",
  "DAEMON",
  "DESKTOP",
  "FRONTEND",
  "MOBILE",
  "SERVER",
  "SERVICE",
  "SERVICES",
  "WEB",
]);

type IssueIdentity = {
  identifierPrefix?: string;
  number: number;
};

export function normalizeIssuePrefix(value?: string) {
  const normalized = sanitizeIssuePrefixInput(value || "");

  return normalized || DEFAULT_ISSUE_PREFIX;
}

export function sanitizeIssuePrefixInput(value: string) {
  return (value || "")
    .trim()
    .toUpperCase()
    .replace(/[^A-Z0-9]/g, "");
}

export function formatIssueId(prefix: string | undefined, number: number) {
  return `${normalizeIssuePrefix(prefix)}-${number}`;
}

export function formatTaskIssueId(task: IssueIdentity) {
  return formatIssueId(task.identifierPrefix, task.number);
}

export function deriveProjectIssuePrefix(name: string) {
  const tokens = projectKeyTokens(name);
  if (tokens.length === 0) {
    return DEFAULT_ISSUE_PREFIX;
  }

  const trimmedTokens = trimProjectKeyTokens(tokens);
  const effectiveTokens = trimmedTokens.length > 0 ? trimmedTokens : tokens;
  const [first] = effectiveTokens;
  if (first.length >= 3) {
    return first.slice(0, 3);
  }

  let candidate = first;
  for (let index = 1; index < effectiveTokens.length && candidate.length < 3; index += 1) {
    candidate += effectiveTokens[index].slice(0, 1);
  }

  return candidate || DEFAULT_ISSUE_PREFIX;
}

function projectKeyTokens(name: string) {
  return (name || "")
    .trim()
    .toUpperCase()
    .split(/[^A-Z0-9]+/)
    .filter(Boolean);
}

function trimProjectKeyTokens(tokens: string[]) {
  if (tokens.length <= 1) {
    return tokens;
  }

  let end = tokens.length;
  while (
    end > 1 &&
    PROJECT_KEY_IGNORED_TRAILING_TOKENS.has(tokens[end - 1])
  ) {
    end -= 1;
  }

  return tokens.slice(0, end);
}
