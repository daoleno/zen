/**
 * Provider-neutral tool-call semantics for Codex / Grok / Cursor / Claude timelines.
 *
 * Safety invariants:
 * - Never evaluate JavaScript from provider payloads.
 * - Collapsed labels expose only bounded, safely-derived command intent or
 *   distinctive paths; never patches, secret-looking paths, URLs, tokens, or
 *   unclassified raw arguments.
 * - Raw provider/tool identifiers stay secondary (expansion only).
 */

export type SemanticActionKind =
  | "read_files"
  | "search_code"
  | "run_command"
  | "update_files"
  | "update_plan"
  | "view_image"
  | "test_app"
  | "wait"
  | "use_tool";

export type SemanticActionStatus = "running" | "done" | "failed" | "blocked";

export interface NestedToolCall {
  name: string;
  rawArgs: string;
  object?: Record<string, unknown>;
  text?: string;
}

export interface SemanticAction {
  kind: SemanticActionKind;
  /** Quiet list-first label for the collapsed row. */
  label: string;
  /** Safe, bounded object/command summary appended to the collapsed label. */
  target?: string;
  /** Accessibility label; may be slightly more descriptive than label. */
  accessibilityLabel: string;
  /** Provider/tool identifier shown only after expansion. */
  providerToolId?: string;
  status: SemanticActionStatus;
  /** Optional quiet secondary summary with no secrets. */
  quietDetail?: string;
  children?: SemanticAction[];
}

export interface ToolCallPresentationInput {
  toolName?: string;
  title?: string;
  input?: string;
  command?: string;
  status?: string;
  exitCode?: number;
  files?: string[];
  kind?: string;
}

const CONST_STRING_RE =
  /(?:const|let|var)\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*("(?:\\.|[^"\\])*")/gm;

const SECRET_PATH_RE =
  /(^|\/)(\.env|\.env\..+|credentials|secrets?|token|auth\.json|id_rsa|id_ed25519)(\/|$)/i;
const TOKEN_RE =
  /\b(sk-[a-z0-9_-]{8,}|ghp_[a-z0-9]{8,}|xox[baprs]-[a-z0-9-]{8,})\b/i;
const URL_RE = /https?:\/\/[^\s"'`]+/i;
const SECRET_COMMAND_RE =
  /(?:(?:^|\s)(?:[A-Z0-9_]*(?:TOKEN|PASSWORD|PASSWD|API_KEY|SECRET|AUTHORIZATION|COOKIE)[A-Z0-9_]*\s*=\s*\S+|--?(?:token|password|passwd|api[_-]?key|authorization|cookie|client[_-]?secret)(?:=|\s+)\S+|authorization:\s*\S+|bearer\s+\S+)|(?:^|[\s/])\.env(?:\.[^\s/]*)?(?:\s|$))/i;
const COLLAPSED_TARGET_LIMIT = 64;

export function isExecWrapperToolName(name?: string): boolean {
  const normalized = stripToolNamespace(name || "");
  return normalized === "exec";
}

export function stripToolNamespace(name: string): string {
  return name
    .trim()
    .replace(/^functions\./, "")
    .replace(/^tools\./, "");
}

export function parseExecWrapperCalls(input?: string): NestedToolCall[] {
  if (!input || !input.trim()) {
    return [];
  }
  const source = input.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
  const assignments = new Map<string, string>();
  for (const match of source.matchAll(CONST_STRING_RE)) {
    const decoded = decodeJSStringLiteral(match[2] || "");
    if (match[1] && decoded != null) {
      assignments.set(match[1], decoded);
    }
  }

  const calls: NestedToolCall[] = [];
  for (const site of findExecToolCallSites(source)) {
    const rawArgs = extractBalancedArgs(source, site.openParen);
    if (rawArgs == null) {
      continue;
    }
    const parsed = parseExecWrapperArgs(rawArgs.trim(), assignments);
    calls.push({
      name: site.name,
      rawArgs: rawArgs.trim(),
      object: parsed.object,
      text: parsed.text,
    });
  }
  return calls;
}

type ExecToolCallSite = {
  name: string;
  openParen: number;
};

/** Lexical scan: only count tools.NAME( in executable code positions. */
export function findExecToolCallSites(source: string): ExecToolCallSite[] {
  const sites: ExecToolCallSite[] = [];
  let i = 0;
  while (i < source.length) {
    const ch = source[i];
    if (ch === '"' || ch === "'") {
      i = skipJSQuotedString(source, i);
      continue;
    }
    if (ch === "`") {
      i = skipJSTemplateLiteral(source, i);
      continue;
    }
    if (ch === "/" && source[i + 1] === "/") {
      i = skipJSLineComment(source, i);
      continue;
    }
    if (ch === "/" && source[i + 1] === "*") {
      i = skipJSBlockComment(source, i);
      continue;
    }
    if (
      hasJSIdentPrefix(source, i, "tools") &&
      !isJSIdentPartAt(source, i - 1) &&
      source[i + 5] === "."
    ) {
      const nameStart = i + 6;
      let nameEnd = nameStart;
      while (nameEnd < source.length && /[A-Za-z0-9_$]/.test(source[nameEnd])) {
        nameEnd += 1;
      }
      if (nameEnd > nameStart) {
        let j = nameEnd;
        while (j < source.length && /[ \t\n\r]/.test(source[j])) {
          j += 1;
        }
        if (source[j] === "(") {
          sites.push({
            name: source.slice(nameStart, nameEnd),
            openParen: j,
          });
          i = j;
          continue;
        }
      }
    }
    i += 1;
  }
  return sites;
}

function skipJSQuotedString(source: string, start: number): number {
  const quote = source[start];
  let escaped = false;
  for (let i = start + 1; i < source.length; i += 1) {
    const ch = source[i];
    if (escaped) {
      escaped = false;
      continue;
    }
    if (ch === "\\") {
      escaped = true;
      continue;
    }
    if (ch === quote) {
      return i + 1;
    }
    if (ch === "\n") {
      return i;
    }
  }
  return source.length;
}

function skipJSTemplateLiteral(source: string, start: number): number {
  let escaped = false;
  for (let i = start + 1; i < source.length;) {
    const ch = source[i];
    if (escaped) {
      escaped = false;
      i += 1;
      continue;
    }
    if (ch === "\\") {
      escaped = true;
      i += 1;
      continue;
    }
    if (ch === "`") {
      return i + 1;
    }
    if (ch === "$" && source[i + 1] === "{") {
      i = skipJSTemplateExpression(source, i + 2);
      continue;
    }
    i += 1;
  }
  return source.length;
}

function skipJSTemplateExpression(source: string, start: number): number {
  let depth = 1;
  let i = start;
  while (i < source.length && depth > 0) {
    const ch = source[i];
    if (ch === '"' || ch === "'") {
      i = skipJSQuotedString(source, i);
      continue;
    }
    if (ch === "`") {
      i = skipJSTemplateLiteral(source, i);
      continue;
    }
    if (ch === "/" && source[i + 1] === "/") {
      i = skipJSLineComment(source, i);
      continue;
    }
    if (ch === "/" && source[i + 1] === "*") {
      i = skipJSBlockComment(source, i);
      continue;
    }
    if (ch === "{") {
      depth += 1;
      i += 1;
      continue;
    }
    if (ch === "}") {
      depth -= 1;
      i += 1;
      continue;
    }
    i += 1;
  }
  return i;
}

function skipJSLineComment(source: string, start: number): number {
  let i = start + 2;
  while (i < source.length && source[i] !== "\n") {
    i += 1;
  }
  return i < source.length ? i + 1 : i;
}

function skipJSBlockComment(source: string, start: number): number {
  let i = start + 2;
  while (i + 1 < source.length) {
    if (source[i] === "*" && source[i + 1] === "/") {
      return i + 2;
    }
    i += 1;
  }
  return source.length;
}

function hasJSIdentPrefix(
  source: string,
  index: number,
  ident: string,
): boolean {
  if (index < 0 || index + ident.length > source.length) {
    return false;
  }
  if (source.slice(index, index + ident.length) !== ident) {
    return false;
  }
  return !isJSIdentPartAt(source, index + ident.length);
}

function isJSIdentPartAt(source: string, index: number): boolean {
  if (index < 0 || index >= source.length) {
    return false;
  }
  return /[A-Za-z0-9_$]/.test(source[index]);
}

export function buildSemanticActions(
  input: ToolCallPresentationInput,
): SemanticAction[] {
  const status = normalizeStatus(input.status, input.exitCode);
  const toolName = stripToolNamespace(input.toolName || input.title || "");

  if (
    input.kind === "command" ||
    toolName === "exec_command" ||
    toolName === "shell_command"
  ) {
    return [
      semanticFromCommand(
        input.command || commandFromInput(input.input),
        status,
        toolName || "exec_command",
      ),
    ];
  }
  if (input.kind === "patch" || toolName === "apply_patch") {
    return [
      semanticFromPatch(input.files || pathsFromToolInput(input.input), status),
    ];
  }
  if (input.kind === "plan" || toolName === "update_plan") {
    return [action("update_plan", status, "update_plan")];
  }

  if (isExecWrapperToolName(toolName) || toolName.startsWith("multi:")) {
    const nested = parseExecWrapperCalls(input.input);
    if (nested.length > 0) {
      return semanticFromNestedCalls(nested, status);
    }
    if (toolName.startsWith("multi:")) {
      const names = toolName.slice("multi:".length).split(",").filter(Boolean);
      if (names.length > 0) {
        return [
          summarizeActions(
            names.map((name) => semanticFromToolName(name, undefined, status)),
            status,
          ),
        ];
      }
    }
  }

  if (toolName.includes("multi_tool_use.parallel")) {
    const nestedNames = parallelToolNames(input.input);
    if (nestedNames.length > 0) {
      return [
        summarizeActions(
          nestedNames.map((name) =>
            semanticFromToolName(name, undefined, status),
          ),
          status,
        ),
      ];
    }
  }

  return [
    semanticFromToolName(toolName || "tool", input.input, status, input.files),
  ];
}

export function primarySemanticAction(
  input: ToolCallPresentationInput,
): SemanticAction {
  const actions = buildSemanticActions(input);
  return (
    actions[0] ||
    action("use_tool", normalizeStatus(input.status, input.exitCode), "tool")
  );
}

export function collapsedToolLabel(input: ToolCallPresentationInput): {
  title: string;
  detail?: string;
  accessibilityLabel: string;
  children?: SemanticAction[];
  providerToolId?: string;
} {
  const semantic = primarySemanticAction(input);
  return {
    title: semanticActionTitle(semantic),
    detail: semantic.quietDetail,
    accessibilityLabel: semantic.accessibilityLabel,
    children: semantic.children,
    providerToolId: semantic.providerToolId,
  };
}

export function semanticActionTitle(action: SemanticAction): string {
  return action.target ? `${action.label} ${action.target}` : action.label;
}

function semanticFromNestedCalls(
  calls: NestedToolCall[],
  status: SemanticActionStatus,
): SemanticAction[] {
  const children = calls.map((call) => nestedCallToSemantic(call, status));
  if (children.length === 1) {
    return children;
  }
  return [summarizeActions(children, status)];
}

function nestedCallToSemantic(
  call: NestedToolCall,
  status: SemanticActionStatus,
): SemanticAction {
  switch (call.name) {
    case "exec_command":
    case "shell_command":
      return semanticFromCommand(commandFromNested(call), status, call.name);
    case "apply_patch":
      return semanticFromPatch(patchFiles(call), status);
    case "update_plan":
      return action("update_plan", status, call.name);
    case "view_image":
      return action(
        "view_image",
        status,
        call.name,
        undefined,
        collapsedFileTarget([
          stringField(call.object || {}, "path") ||
            stringField(call.object || {}, "image_url"),
        ]),
      );
    default:
      if (call.name.startsWith("browser_")) {
        return action("test_app", status, call.name);
      }
      return semanticFromToolName(call.name, undefined, status);
  }
}

function semanticFromCommand(
  command: string | undefined,
  status: SemanticActionStatus,
  providerToolId: string,
): SemanticAction {
  const value = command || "";
  const kind = classifyCommand(value);
  const label = commandActionLabel(value, kind);
  return action(
    kind,
    status,
    providerToolId,
    label,
    collapsedCommandTarget(value, kind, label),
  );
}

function semanticFromPatch(
  files: string[],
  status: SemanticActionStatus,
): SemanticAction {
  return action(
    "update_files",
    status,
    "apply_patch",
    undefined,
    collapsedFileTarget(files),
  );
}

function semanticFromToolName(
  rawName: string,
  input: string | undefined,
  status: SemanticActionStatus,
  files?: string[],
): SemanticAction {
  const name = stripToolNamespace(rawName);
  const lower = name.toLowerCase();

  if (lower === "view_image") {
    return action(
      "view_image",
      status,
      name,
      undefined,
      collapsedFileTarget(files?.length ? files : pathsFromToolInput(input)),
    );
  }
  if (
    lower === "wait" ||
    lower === "write_stdin" ||
    lower === "await" ||
    lower === "awaitshell" ||
    lower === "await_shell"
  ) {
    return action("wait", status, name);
  }
  if (
    lower === "apply_patch" ||
    lower === "edit" ||
    lower === "write" ||
    lower === "multiedit"
  ) {
    return action(
      "update_files",
      status,
      name,
      undefined,
      collapsedFileTarget(files?.length ? files : pathsFromToolInput(input)),
    );
  }
  if (lower === "update_plan" || lower === "todowrite") {
    return action("update_plan", status, name);
  }
  if (lower === "grep" || lower === "rg" || lower.includes("search")) {
    return action(
      "search_code",
      status,
      name,
      undefined,
      safeCollapsedValue(queryFromToolInput(input)),
    );
  }
  if (
    lower === "read" ||
    lower === "read_file" ||
    lower === "glob" ||
    lower === "list_files" ||
    lower === "ls"
  ) {
    return action(
      "read_files",
      status,
      name,
      undefined,
      collapsedFileTarget(files?.length ? files : pathsFromToolInput(input)),
    );
  }
  if (lower === "shell" || lower === "bash" || lower === "exec_command") {
    const command = commandFromInput(input) || undefined;
    return semanticFromCommand(command, status, name);
  }
  if (
    lower.startsWith("browser_") ||
    lower.includes("agent-browser") ||
    lower.includes("playwright")
  ) {
    return action("test_app", status, name);
  }

  const human = humanizeToolName(name);
  return {
    kind: "use_tool",
    label: `Use ${human}`,
    accessibilityLabel: `Use ${human}`,
    providerToolId: name,
    status,
  };
}

function summarizeActions(
  children: SemanticAction[],
  status: SemanticActionStatus,
): SemanticAction {
  const uniqueKinds = [...new Set(children.map((child) => child.kind))];
  const allCommandLike = children.every(
    (child) => child.kind === "run_command" || child.kind === "test_app",
  );
  let label = "Use";
  if (allCommandLike && children.length > 1) {
    label = "Run";
  } else if (uniqueKinds.length === 1) {
    label = pluralLabel(uniqueKinds[0], children.length);
  } else if (children.length > 1) {
    label = "Use";
  }
  return {
    kind: allCommandLike
      ? "run_command"
      : uniqueKinds.length === 1
        ? uniqueKinds[0]
        : "use_tool",
    label,
    target:
      children.length > 1 && children[0]?.target
        ? `${children[0].target} + ${children.length - 1}`
        : children[0]?.target,
    accessibilityLabel: `${label}. ${children.map(semanticActionTitle).join(", ")}`,
    providerToolId: uniqueProviderToolIds(children),
    status,
    children,
  };
}

function uniqueProviderToolIds(children: SemanticAction[]): string | undefined {
  const seen = new Set<string>();
  const ids: string[] = [];
  for (const child of children) {
    const id = (child.providerToolId || "").trim();
    if (!id || seen.has(id)) {
      continue;
    }
    seen.add(id);
    ids.push(id);
  }
  return ids.length > 0 ? ids.join(",") : undefined;
}

function action(
  kind: SemanticActionKind,
  status: SemanticActionStatus,
  providerToolId?: string,
  labelOverride?: string,
  target?: string,
): SemanticAction {
  const label = labelOverride || defaultLabel(kind);
  const safeTarget = safeCollapsedValue(target);
  return {
    kind,
    label,
    target: safeTarget,
    accessibilityLabel: safeTarget ? `${label} ${safeTarget}` : label,
    providerToolId,
    status,
  };
}

function defaultLabel(kind: SemanticActionKind): string {
  switch (kind) {
    case "read_files":
      return "Read";
    case "search_code":
      return "Search";
    case "run_command":
      return "Run";
    case "update_files":
      return "Edit";
    case "update_plan":
      return "Plan";
    case "view_image":
      return "Open";
    case "test_app":
      return "Test";
    case "wait":
      return "Wait";
    default:
      return "Use";
  }
}

function pluralLabel(kind: SemanticActionKind, count: number): string {
  if (count <= 1) {
    return defaultLabel(kind);
  }
  switch (kind) {
    case "run_command":
      return "Run";
    case "read_files":
      return "Read";
    case "search_code":
      return "Search";
    case "update_files":
      return "Edit";
    case "test_app":
      return "Test";
    default:
      return "Use";
  }
}

function classifyCommand(command: string): SemanticActionKind {
  const lower = command.toLowerCase();
  if (!lower.trim()) {
    return "run_command";
  }
  if (
    /\b(go test|bun test|npm test|pnpm test|yarn test|jest|vitest|pytest|agent-browser|playwright)\b/.test(
      lower,
    )
  ) {
    return "test_app";
  }
  if (/\b(rg|grep|ag|ack)\b/.test(lower) || /\bfind\b/.test(lower)) {
    return "search_code";
  }
  if (/\b(cat|sed|nl|less|head|tail)\b/.test(lower)) {
    return "read_files";
  }
  return "run_command";
}

function commandActionLabel(command: string, kind: SemanticActionKind): string {
  if (kind === "test_app") {
    return "Run";
  }
  if (isBuildCommand(command)) {
    return "Build";
  }
  return defaultLabel(kind);
}

function collapsedCommandTarget(
  command: string,
  kind: SemanticActionKind,
  label: string,
): string | undefined {
  const normalized = command.replace(/\s+/g, " ").trim();
  if (!normalized || isSecretBearingCommand(normalized)) {
    return undefined;
  }
  const segment = meaningfulCommandSegment(normalized, kind, label);
  const tokens = simpleCommandTokens(segment);

  if (kind === "search_code") {
    return safeCollapsedValue(searchQueryFromTokens(tokens));
  }
  if (kind === "read_files") {
    const path = readPathFromTokens(tokens);
    return path ? distinctivePath(path) : undefined;
  }
  if (kind === "test_app") {
    return safeCollapsedValue(testCommandSummary(tokens));
  }
  if (label === "Build") {
    return safeCollapsedValue(buildCommandTarget(tokens));
  }
  return safeCollapsedValue(genericCommandSummary(tokens));
}

function genericCommandSummary(tokens: string[]): string {
  return truncateCollapsed(
    tokens
      .map((token) => {
        const absolute = token.startsWith("/") || /^[A-Za-z]:[\\/]/.test(token);
        return absolute && looksLikePath(token)
          ? distinctivePath(token) || pathBasename(token)
          : token;
      })
      .join(" "),
  );
}

function meaningfulCommandSegment(
  command: string,
  kind: SemanticActionKind,
  label: string,
): string {
  const segments = command
    .split(/\s*(?:&&|\|\||;)\s*/)
    .map((segment) => segment.trim())
    .filter(Boolean);
  return (
    segments.find((segment) =>
      label === "Build"
        ? isBuildCommand(segment)
        : classifyCommand(segment) === kind && !/^cd\s/.test(segment),
    ) ||
    segments.find((segment) => !/^(?:cd|export)\s/.test(segment)) ||
    command
  );
}

function isBuildCommand(command: string): boolean {
  return /(?:^|\s)(?:go\s+build|(?:bun|npm|pnpm|yarn)\s+(?:run\s+)?[^\s]*(?:build|bundle|export)[^\s]*|(?:\.\/)?gradlew\s+[^\s]*(?:assemble|build)[^\s]*|xcodebuild(?:\s|$)|expo\s+export(?:\s|$))/i.test(
    command,
  );
}

function buildCommandTarget(tokens: string[]): string {
  const lower = tokens.map((token) => token.toLowerCase());
  const scriptRunner = lower.findIndex(
    (token, index) =>
      ["bun", "npm", "pnpm", "yarn"].includes(token) &&
      lower[index + 1] === "run",
  );
  if (scriptRunner >= 0) {
    const script = tokens[scriptRunner + 2] || "";
    const target = script
      .replace(/^(?:build|bundle|export)[:-]?/i, "")
      .replace(/[:-]?(?:build|bundle|export)$/i, "");
    return target || script;
  }
  const go = lower.findIndex(
    (token, index) => token === "go" && lower[index + 1] === "build",
  );
  if (go >= 0) {
    const target = tokens.slice(go + 2).find((token) => !token.startsWith("-"));
    return target ? distinctivePath(target) || target : "Go";
  }
  const gradleTask = tokens.find((token) => /^(?:assemble|build)/i.test(token));
  if (gradleTask) return gradleTask;
  if (lower.includes("xcodebuild")) return "iOS";
  if (lower.includes("expo") && lower.includes("export")) return "Expo";
  return truncateCollapsed(tokens.join(" "));
}

function testCommandSummary(tokens: string[]): string {
  const lower = tokens.map((token) => token.toLowerCase());
  const go = lower.findIndex(
    (token, index) => token === "go" && lower[index + 1] === "test",
  );
  if (go >= 0) {
    const target = tokens.slice(go + 2).find((token) => !token.startsWith("-"));
    return ["go test", target].filter(Boolean).join(" ");
  }
  const runner = lower.findIndex((token) =>
    ["bun", "npm", "pnpm", "yarn"].includes(token),
  );
  if (runner >= 0) {
    const testIndex = lower.indexOf("test", runner + 1);
    if (testIndex >= 0) {
      const target = tokens
        .slice(testIndex + 1)
        .find((token) => !token.startsWith("-"));
      return [tokens[runner], "test", target].filter(Boolean).join(" ");
    }
  }
  const testExecutable = lower.findIndex((token) =>
    ["jest", "vitest", "pytest", "playwright", "agent-browser"].includes(token),
  );
  if (testExecutable >= 0) {
    const target = tokens
      .slice(testExecutable + 1)
      .find((token) => !token.startsWith("-"));
    return [tokens[testExecutable], target].filter(Boolean).join(" ");
  }
  return truncateCollapsed(tokens.join(" "));
}

function searchQueryFromTokens(tokens: string[]): string {
  const executables = new Set(["rg", "grep", "ag", "ack", "find"]);
  const executableIndex = tokens.findIndex((token) =>
    executables.has(pathBasename(token).toLowerCase()),
  );
  if (executableIndex < 0) {
    return "";
  }
  const executable = pathBasename(tokens[executableIndex]).toLowerCase();
  if (executable === "find") {
    const nameIndex = tokens.findIndex(
      (token) => token === "-name" || token === "-iname",
    );
    return nameIndex >= 0 ? tokens[nameIndex + 1] || "" : "";
  }
  const valueOptions = new Set([
    "-e",
    "--regexp",
    "-g",
    "--glob",
    "-t",
    "--type",
    "-m",
    "--max-count",
    "-A",
    "-B",
    "-C",
  ]);
  for (let index = executableIndex + 1; index < tokens.length; index += 1) {
    const token = tokens[index];
    if (valueOptions.has(token)) {
      if (token === "-e" || token === "--regexp") {
        return tokens[index + 1] || "";
      }
      index += 1;
      continue;
    }
    if (!token.startsWith("-")) {
      return token;
    }
  }
  return "";
}

function readPathFromTokens(tokens: string[]): string {
  const executables = new Set(["cat", "sed", "nl", "less", "head", "tail"]);
  const executableIndex = tokens.findIndex((token) =>
    executables.has(pathBasename(token).toLowerCase()),
  );
  if (executableIndex < 0) {
    return "";
  }
  const candidates = tokens
    .slice(executableIndex + 1)
    .filter((token) => !token.startsWith("-") && looksLikePath(token));
  return candidates[candidates.length - 1] || "";
}

/** Bounded token hints for summaries only; this is not shell parsing. */
function simpleCommandTokens(value: string): string[] {
  return (value.match(/"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s]+/g) || []).map(
    (token) => {
      const quoted =
        (token.startsWith('"') && token.endsWith('"')) ||
        (token.startsWith("'") && token.endsWith("'"));
      return quoted ? token.slice(1, -1) : token;
    },
  );
}

function isSecretBearingCommand(command: string): boolean {
  return (
    TOKEN_RE.test(command) ||
    URL_RE.test(command) ||
    SECRET_PATH_RE.test(command) ||
    SECRET_COMMAND_RE.test(command)
  );
}

function normalizeStatus(
  status?: string,
  exitCode?: number,
): SemanticActionStatus {
  const normalized = (status || "").trim().toLowerCase();
  if (normalized === "running" || normalized === "in_progress") {
    return "running";
  }
  if (normalized === "blocked") {
    return "blocked";
  }
  if (normalized === "failed" || normalized === "error") {
    return "failed";
  }
  if (
    ["done", "completed", "success", "succeeded", "passed"].includes(normalized)
  ) {
    return "done";
  }
  if (exitCode != null && exitCode !== 0) {
    return "failed";
  }
  return "done";
}

function commandFromNested(call: NestedToolCall): string {
  if (call.object) {
    const cmd =
      stringField(call.object, "cmd") || stringField(call.object, "command");
    if (cmd) {
      return cmd;
    }
  }
  return commandFromInput(call.rawArgs);
}

function commandFromInput(input?: string): string {
  if (!input) {
    return "";
  }
  try {
    const parsed = JSON.parse(input);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return (
        stringField(parsed as Record<string, unknown>, "cmd") ||
        stringField(parsed as Record<string, unknown>, "command")
      );
    }
  } catch {
    // not JSON
  }
  return "";
}

function pathsFromToolInput(input?: string): string[] {
  if (!input) {
    return [];
  }
  try {
    const parsed = JSON.parse(input);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return [];
    }
    const record = parsed as Record<string, unknown>;
    const paths: string[] = patchPaths(stringField(record, "patch"));
    for (const key of [
      "path",
      "file_path",
      "filePath",
      "filename",
      "file",
      "target_file",
      "targetFile",
    ]) {
      const value = record[key];
      if (typeof value === "string" && looksLikePath(value)) {
        paths.push(value.trim());
      }
    }
    for (const key of ["paths", "files", "file_paths"]) {
      const value = record[key];
      if (!Array.isArray(value)) {
        continue;
      }
      for (const item of value) {
        if (typeof item === "string" && looksLikePath(item)) {
          paths.push(item.trim());
        }
      }
    }
    return paths;
  } catch {
    return patchPaths(input);
  }
}

function patchPaths(value: string): string[] {
  return [...value.matchAll(/^\*\*\* (?:Update|Add|Delete) File:\s+(.+)$/gm)]
    .map((match) => match[1]?.trim() || "")
    .filter(Boolean);
}

function queryFromToolInput(input?: string): string {
  if (!input) {
    return "";
  }
  try {
    const parsed = JSON.parse(input);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return "";
    }
    const record = parsed as Record<string, unknown>;
    return (
      stringField(record, "pattern") ||
      stringField(record, "query") ||
      stringField(record, "glob_pattern") ||
      stringField(record, "glob")
    );
  } catch {
    return "";
  }
}

function collapsedFileTarget(files: string[]): string | undefined {
  const targets = [...new Set(files.map(distinctivePath).filter(Boolean))];
  if (targets.length === 0) {
    return undefined;
  }
  return targets.length === 1
    ? targets[0]
    : `${targets[0]} + ${targets.length - 1}`;
}

/** A collapsed-only path projection; expanded details retain the original. */
export function distinctivePath(value: string): string | undefined {
  const trimmed = value.trim();
  if (
    !trimmed ||
    TOKEN_RE.test(trimmed) ||
    URL_RE.test(trimmed) ||
    SECRET_PATH_RE.test(trimmed)
  ) {
    return undefined;
  }
  const normalized = trimmed.replace(/\\/g, "/");
  const absolute =
    normalized.startsWith("/") || /^[A-Za-z]:\//.test(normalized);
  const segments = normalized
    .replace(/^[A-Za-z]:/, "")
    .split("/")
    .filter((segment) => segment && segment !== "." && segment !== "..");
  if (segments.length === 0) {
    return undefined;
  }
  const anchors = new Set([
    "app",
    "daemon",
    "docs",
    "scripts",
    "src",
    "test",
    "tests",
    "packages",
    "internal",
  ]);
  const anchor = segments.findIndex((segment) => anchors.has(segment));
  const projected = absolute
    ? anchor >= 0
      ? segments.slice(anchor).join("/")
      : segments[segments.length - 1]
    : segments.join("/");
  return safeCollapsedValue(projected);
}

function looksLikePath(value: string): boolean {
  const trimmed = value.trim();
  return Boolean(
    trimmed &&
    trimmed.length <= 400 &&
    !trimmed.startsWith("{") &&
    !trimmed.startsWith("[") &&
    (trimmed.startsWith("/") ||
      trimmed.startsWith("./") ||
      trimmed.startsWith("../") ||
      trimmed.startsWith("~/") ||
      trimmed.includes("/") ||
      /\.[A-Za-z0-9]{1,10}$/.test(trimmed)),
  );
}

function pathBasename(value: string): string {
  const parts = value.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] || value;
}

function safeCollapsedValue(value?: string): string | undefined {
  if (!value?.trim()) {
    return undefined;
  }
  const compact = truncateCollapsed(value.replace(/\s+/g, " ").trim());
  return isUnsafeCollapsedDetail(compact) ? undefined : compact;
}

function truncateCollapsed(value: string): string {
  const chars = Array.from(value);
  return chars.length <= COLLAPSED_TARGET_LIMIT
    ? value
    : `${chars.slice(0, COLLAPSED_TARGET_LIMIT - 1).join("")}…`;
}

function patchFiles(call: NestedToolCall): string[] {
  const text = call.text || stringField(call.object || {}, "patch") || "";
  if (!text) {
    return [];
  }
  return patchPaths(text);
}

function parallelToolNames(input?: string): string[] {
  if (!input) {
    return [];
  }
  try {
    const parsed = JSON.parse(input);
    if (!parsed || typeof parsed !== "object") {
      return [];
    }
    const toolUses = (parsed as { tool_uses?: unknown }).tool_uses;
    if (!Array.isArray(toolUses)) {
      return [];
    }
    return toolUses
      .map((toolUse) => {
        if (!toolUse || typeof toolUse !== "object") {
          return "";
        }
        const name = (toolUse as { recipient_name?: unknown }).recipient_name;
        return typeof name === "string" ? stripToolNamespace(name) : "";
      })
      .filter(Boolean);
  } catch {
    return [];
  }
}

function parseExecWrapperArgs(
  raw: string,
  assignments: Map<string, string>,
): { object?: Record<string, unknown>; text?: string } {
  if (!raw) {
    return {};
  }
  if (raw.startsWith('"') || raw.startsWith("'") || raw.startsWith("`")) {
    const text = decodeJSStringLiteral(raw);
    return text != null ? { text } : {};
  }
  if (isSimpleIdent(raw)) {
    const text = assignments.get(raw);
    return text != null ? { text } : {};
  }
  if (raw.startsWith("{")) {
    const normalized = normalizeJSObjectLiteral(raw);
    try {
      const object = JSON.parse(normalized) as Record<string, unknown>;
      return { object };
    } catch {
      return { object: extractLooseObjectFields(raw) };
    }
  }
  return {};
}

export function extractBalancedArgs(
  source: string,
  openParen: number,
): string | null {
  if (source[openParen] !== "(") {
    return null;
  }
  let depth = 0;
  let inString: string | null = null;
  let escaped = false;
  for (let index = openParen; index < source.length; index += 1) {
    const ch = source[index];
    if (inString) {
      if (escaped) {
        escaped = false;
        continue;
      }
      if (ch === "\\" && inString !== "`") {
        escaped = true;
        continue;
      }
      if (ch === inString) {
        inString = null;
      }
      continue;
    }
    if (ch === '"' || ch === "'" || ch === "`") {
      inString = ch;
      continue;
    }
    if (ch === "(") {
      depth += 1;
      continue;
    }
    if (ch === ")") {
      depth -= 1;
      if (depth === 0) {
        return source.slice(openParen + 1, index);
      }
    }
  }
  return null;
}

export function decodeJSStringLiteral(value: string): string | null {
  const trimmed = value.trim();
  if (trimmed.length < 2) {
    return null;
  }
  const quote = trimmed[0];
  if (
    (quote !== '"' && quote !== "'" && quote !== "`") ||
    trimmed[trimmed.length - 1] !== quote
  ) {
    return null;
  }
  const inner = trimmed.slice(1, -1);
  let out = "";
  let escaped = false;
  for (let index = 0; index < inner.length; index += 1) {
    const ch = inner[index];
    if (escaped) {
      switch (ch) {
        case "n":
          out += "\n";
          break;
        case "r":
          out += "\r";
          break;
        case "t":
          out += "\t";
          break;
        case "\\":
        case '"':
        case "'":
        case "`":
          out += ch;
          break;
        case "u": {
          const hex = inner.slice(index + 1, index + 5);
          if (/^[0-9a-fA-F]{4}$/.test(hex)) {
            out += String.fromCharCode(parseInt(hex, 16));
            index += 4;
          } else {
            out += "u";
          }
          break;
        }
        default:
          out += ch;
      }
      escaped = false;
      continue;
    }
    if (ch === "\\") {
      escaped = true;
      continue;
    }
    out += ch;
  }
  if (escaped) {
    return null;
  }
  return out;
}

function normalizeJSObjectLiteral(value: string): string {
  let out = "";
  let inString: string | null = null;
  let escaped = false;
  for (let index = 0; index < value.length;) {
    const ch = value[index];
    if (inString) {
      out += ch;
      if (escaped) {
        escaped = false;
        index += 1;
        continue;
      }
      if (ch === "\\") {
        escaped = true;
        index += 1;
        continue;
      }
      if (ch === inString) {
        inString = null;
      }
      index += 1;
      continue;
    }
    if (ch === '"' || ch === "'" || ch === "`") {
      inString = ch;
      out += ch;
      index += 1;
      continue;
    }
    if (/[A-Za-z_$]/.test(ch)) {
      const start = index;
      index += 1;
      while (index < value.length && /[A-Za-z0-9_$]/.test(value[index])) {
        index += 1;
      }
      const ident = value.slice(start, index);
      const rest = value.slice(index).replace(/^[ \t\n\r]+/, "");
      if (rest.startsWith(":") && !isJSKeyword(ident)) {
        out += `"${ident}"`;
        continue;
      }
      out += ident;
      continue;
    }
    out += ch;
    index += 1;
  }
  return out;
}

function extractLooseObjectFields(raw: string): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  const re =
    /(?:"([^"]+)"|([A-Za-z_][A-Za-z0-9_]*))\s*:\s*("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*')/g;
  for (const match of raw.matchAll(re)) {
    const key = match[1] || match[2];
    const decoded = decodeJSStringLiteral(match[3] || "");
    if (key && decoded != null) {
      out[key] = decoded;
    }
  }
  return out;
}

function isSimpleIdent(value: string): boolean {
  return /^[A-Za-z_$][A-Za-z0-9_$]*$/.test(value.trim());
}

function isJSKeyword(value: string): boolean {
  return [
    "true",
    "false",
    "null",
    "undefined",
    "new",
    "await",
    "async",
    "function",
    "return",
    "const",
    "let",
    "var",
  ].includes(value);
}

function stringField(record: Record<string, unknown>, key: string): string {
  const value = record[key];
  return typeof value === "string" ? value.trim() : "";
}

function humanizeToolName(value: string): string {
  const cleaned = stripToolNamespace(value)
    .replace(/^mcp__/, "")
    .replace(/__/g, " ")
    .replace(/_/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  if (!cleaned) {
    return "tool";
  }
  return cleaned.replace(/\b\w/g, (letter) => letter.toUpperCase());
}

/** True when a candidate detail string is unsafe for collapsed labels. */
export function isUnsafeCollapsedDetail(value?: string): boolean {
  if (!value) {
    return false;
  }
  const trimmed = value.trim();
  if (!trimmed) {
    return false;
  }
  if (trimmed.startsWith("{") || trimmed.startsWith("[")) {
    return true;
  }
  if (trimmed.includes("*** Begin Patch") || trimmed.includes("tools.")) {
    return true;
  }
  if (
    URL_RE.test(trimmed) ||
    TOKEN_RE.test(trimmed) ||
    SECRET_PATH_RE.test(trimmed)
  ) {
    return true;
  }
  if (trimmed.length > 80 && /[\\/]/.test(trimmed)) {
    return true;
  }
  return false;
}
