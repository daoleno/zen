/**
 * Provider-neutral tool-call semantics for Codex / Grok / Cursor timelines.
 *
 * Safety invariants:
 * - Never evaluate JavaScript from provider payloads.
 * - Collapsed labels never expose command bodies, patches, secret-looking paths,
 *   URLs, tokens, or raw arguments.
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
const TOKEN_RE = /\b(sk-[a-z0-9_-]{8,}|ghp_[a-z0-9]{8,}|xox[baprs]-[a-z0-9-]{8,})\b/i;
const URL_RE = /https?:\/\/[^\s"'`]+/i;

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
    if (ch === "\"" || ch === "'") {
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
      hasJSIdentPrefix(source, i, "tools")
      && !isJSIdentPartAt(source, i - 1)
      && source[i + 5] === "."
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
  for (let i = start + 1; i < source.length; ) {
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
    if (ch === "\"" || ch === "'") {
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

function hasJSIdentPrefix(source: string, index: number, ident: string): boolean {
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

  if (input.kind === "command" || toolName === "exec_command" || toolName === "shell_command") {
    return [semanticFromCommand(input.command || commandFromInput(input.input), status, toolName || "exec_command")];
  }
  if (input.kind === "patch" || toolName === "apply_patch") {
    return [semanticFromPatch(input.files?.length ?? 0, status)];
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
          nestedNames.map((name) => semanticFromToolName(name, undefined, status)),
          status,
        ),
      ];
    }
  }

  return [semanticFromToolName(toolName || "tool", input.input, status)];
}

export function primarySemanticAction(
  input: ToolCallPresentationInput,
): SemanticAction {
  const actions = buildSemanticActions(input);
  return actions[0] || action("use_tool", normalizeStatus(input.status, input.exitCode), "tool");
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
    title: semantic.label,
    detail: semantic.quietDetail,
    accessibilityLabel: semantic.accessibilityLabel,
    children: semantic.children,
    providerToolId: semantic.providerToolId,
  };
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
      return semanticFromPatch(patchFileCount(call), status);
    case "update_plan":
      return action("update_plan", status, call.name);
    case "view_image":
      return action("view_image", status, call.name);
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
  const kind = classifyCommand(command || "");
  return action(kind, status, providerToolId);
}

function semanticFromPatch(
  fileCount: number,
  status: SemanticActionStatus,
): SemanticAction {
  const base = action("update_files", status, "apply_patch");
  if (fileCount > 0) {
    base.quietDetail = fileCount === 1 ? "1 file" : `${fileCount} files`;
    base.accessibilityLabel = `${base.label}, ${base.quietDetail}`;
  }
  return base;
}

function semanticFromToolName(
  rawName: string,
  input: string | undefined,
  status: SemanticActionStatus,
): SemanticAction {
  const name = stripToolNamespace(rawName);
  const lower = name.toLowerCase();

  if (lower === "view_image") {
    return action("view_image", status, name);
  }
  if (lower === "apply_patch" || lower === "edit" || lower === "write" || lower === "multiedit") {
    return action("update_files", status, name);
  }
  if (lower === "update_plan" || lower === "todowrite") {
    return action("update_plan", status, name);
  }
  if (lower === "grep" || lower === "rg" || lower.includes("search")) {
    return action("search_code", status, name);
  }
  if (lower === "read" || lower === "read_file" || lower === "glob" || lower === "list_files") {
    return action("read_files", status, name);
  }
  if (lower === "shell" || lower === "bash" || lower === "exec_command") {
    const command = commandFromInput(input) || undefined;
    return semanticFromCommand(command, status, name);
  }
  if (lower.startsWith("browser_") || lower.includes("agent-browser") || lower.includes("playwright")) {
    return action("test_app", status, name);
  }

  const human = humanizeToolName(name);
  return {
    kind: "use_tool",
    label: status === "running" ? `Using ${human}` : `Used ${human}`,
    accessibilityLabel: status === "running" ? `Using ${human}` : `Used ${human}`,
    providerToolId: name,
    status,
  };
}

function summarizeActions(
  children: SemanticAction[],
  status: SemanticActionStatus,
): SemanticAction {
  const uniqueKinds = [...new Set(children.map((child) => child.kind))];
  const allCommandLike = children.every((child) =>
    child.kind === "run_command" || child.kind === "test_app" || child.providerToolId === "exec_command" || child.providerToolId === "shell_command",
  );
  let label = status === "running" ? "Using tools" : "Used tools";
  if (allCommandLike && children.length > 1) {
    label = status === "running"
      ? `Running ${children.length} commands`
      : `Ran ${children.length} commands`;
  } else if (uniqueKinds.length === 1) {
    label = pluralLabel(uniqueKinds[0], children.length, status);
  } else if (children.length > 1) {
    label = status === "running"
      ? `Running ${children.length} actions`
      : `${children.length} actions`;
  }
  return {
    kind: allCommandLike
      ? "run_command"
      : uniqueKinds.length === 1
        ? uniqueKinds[0]
        : "use_tool",
    label,
    accessibilityLabel: `${label}. ${children.map((child) => child.label).join(", ")}`,
    quietDetail: undefined,
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
): SemanticAction {
  const label = defaultLabel(kind, status);
  return {
    kind,
    label,
    accessibilityLabel: label,
    providerToolId,
    status,
  };
}

function defaultLabel(kind: SemanticActionKind, status: SemanticActionStatus): string {
  const running = status === "running";
  switch (kind) {
    case "read_files":
      return running ? "Reading files" : "Read files";
    case "search_code":
      return running ? "Searching code" : "Searched code";
    case "run_command":
      return running ? "Running a command" : "Ran a command";
    case "update_files":
      return running ? "Updating files" : "Updated files";
    case "update_plan":
      return running ? "Updating the plan" : "Updated the plan";
    case "view_image":
      return running ? "Opening an image" : "Opened an image";
    case "test_app":
      return running ? "Testing the app" : "Tested the app";
    default:
      return running ? "Using a tool" : "Used a tool";
  }
}

function pluralLabel(
  kind: SemanticActionKind,
  count: number,
  status: SemanticActionStatus,
): string {
  if (count <= 1) {
    return defaultLabel(kind, status);
  }
  switch (kind) {
    case "run_command":
      return status === "running" ? `Running ${count} commands` : `Ran ${count} commands`;
    case "read_files":
      return status === "running" ? "Reading files" : "Read files";
    case "search_code":
      return status === "running" ? "Searching code" : "Searched code";
    case "update_files":
      return status === "running" ? "Updating files" : "Updated files";
    case "test_app":
      return status === "running" ? "Testing the app" : "Tested the app";
    default:
      return status === "running" ? `Using ${count} tools` : `Used ${count} tools`;
  }
}

function classifyCommand(command: string): SemanticActionKind {
  const lower = command.toLowerCase();
  if (!lower.trim()) {
    return "run_command";
  }
  if (
    /\b(go test|bun test|npm test|pnpm test|yarn test|jest|vitest|pytest|agent-browser|playwright)\b/.test(lower)
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
  if (normalized === "failed" || normalized === "error" || (exitCode != null && exitCode !== 0)) {
    return "failed";
  }
  return "done";
}

function commandFromNested(call: NestedToolCall): string {
  if (call.object) {
    const cmd = stringField(call.object, "cmd") || stringField(call.object, "command");
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
      return stringField(parsed as Record<string, unknown>, "cmd")
        || stringField(parsed as Record<string, unknown>, "command");
    }
  } catch {
    // not JSON
  }
  return "";
}

function patchFileCount(call: NestedToolCall): number {
  const text = call.text || stringField(call.object || {}, "patch") || "";
  if (!text) {
    return 0;
  }
  const matches = text.match(/\*\*\* (?:Update|Add|Delete) File:/g);
  return matches?.length ?? 0;
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
  if (raw.startsWith("\"") || raw.startsWith("'") || raw.startsWith("`")) {
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

export function extractBalancedArgs(source: string, openParen: number): string | null {
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
    if (ch === "\"" || ch === "'" || ch === "`") {
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
  if ((quote !== "\"" && quote !== "'" && quote !== "`") || trimmed[trimmed.length - 1] !== quote) {
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
        case "\"":
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
  for (let index = 0; index < value.length; ) {
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
    if (ch === "\"" || ch === "'" || ch === "`") {
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
  if (URL_RE.test(trimmed) || TOKEN_RE.test(trimmed) || SECRET_PATH_RE.test(trimmed)) {
    return true;
  }
  if (trimmed.length > 80 && /[\\/]/.test(trimmed)) {
    return true;
  }
  return false;
}
