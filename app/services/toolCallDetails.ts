/**
 * Expanded tool-call details for structured chat.
 * Collapsed labels live in toolCallSemantics; this builds user-facing expand content.
 */

import {
  isExecWrapperToolName,
  parseExecWrapperCalls,
  stripToolNamespace,
  type SemanticActionKind,
  type SemanticActionStatus,
} from "./toolCallSemantics";

export interface ToolDeveloperDetails {
  providerToolId?: string;
  rawInput?: string;
  transport?: Record<string, string>;
}

export interface ExpandedToolDetails {
  kind: SemanticActionKind | "wait";
  quietDetail?: string;
  files?: string[];
  query?: string;
  command?: string;
  statusLine?: string;
  result?: string;
  /** Pure session/empty-chars poll — timeline must not render a card. */
  hideCard?: boolean;
  /** Prefer merging into a linked parent command when set. */
  mergeIntoCommand?: boolean;
  developer?: ToolDeveloperDetails;
}

export interface ToolCallDetailsInput {
  toolName?: string;
  title?: string;
  kind?: string;
  input?: string;
  output?: string;
  body?: string;
  command?: string;
  status?: string;
  exitCode?: number;
  files?: string[];
  semanticKind?: SemanticActionKind | "wait";
}

const TRANSPORT_KEYS = new Set([
  "chunk_id", "wall_time", "wall_time_seconds", "session_id",
  "original_token_count", "token_count", "yield_time_ms", "call_id", "exit_code",
]);

export function isWaitLikeToolName(name?: string): boolean {
  const n = stripToolNamespace(name || "").toLowerCase();
  return n === "wait" || n === "write_stdin" || n === "await"
    || n === "awaitshell" || n === "await_shell";
}

/** Empty-chars / session-only poll used by Codex long-running exec. */
export function isWaitSessionPoll(toolName?: string, input?: string): boolean {
  if (!isWaitLikeToolName(toolName)) return false;
  const parsed = parseJson(input);
  if (!parsed) return false;
  if (Object.prototype.hasOwnProperty.call(parsed, "chars") && parsed.chars === "") {
    return true;
  }
  const keys = Object.keys(parsed);
  const onlyTransport = keys.length > 0 && keys.every((k) =>
    ["session_id", "sessionid", "yield_time_ms", "yieldtimems", "chars"].includes(k.toLowerCase())
  );
  return onlyTransport && !String(parsed.chars || "");
}

export function buildExpandedToolDetails(input: ToolCallDetailsInput): ExpandedToolDetails {
  const toolName = stripToolNamespace(input.toolName || input.title || "");
  const status = normalizeStatus(input.status, input.exitCode);
  const parsedInput = parseJson(input.input);
  const rawOutput = input.output || input.body || "";
  const transport = extractTransport(input.input, rawOutput);
  const kind = input.semanticKind || classifyName(toolName);
  const result = cleanOutput(rawOutput);

  if (isWaitLikeToolName(toolName) || kind === "wait") {
    if (isWaitSessionPoll(toolName, input.input)) {
      return {
        kind: "wait",
        hideCard: true,
        mergeIntoCommand: Boolean(input.command?.trim() || transport.session_id),
        statusLine: waitStatusLine(status, transport.wall_time),
        developer: developerFor(toolName, input.input, transport, kind),
      };
    }
    const running = status === "running" || /process running/i.test(rawOutput);
    const statusLine = waitStatusLine(running ? "running" : status, transport.wall_time);
    return {
      kind: "wait",
      hideCard: false,
      mergeIntoCommand: Boolean(input.command?.trim()) && !result,
      quietDetail: formatDuration(transport.wall_time),
      command: input.command?.trim() || undefined,
      statusLine,
      result: result || undefined,
      developer: developerFor(toolName, input.input, transport, kind),
    };
  }

  const nestedCommand = commandFromNestedExec(toolName, input.input)
    || input.command
    || cmdFrom(parsedInput);
  const files = unique([
    ...(input.files || []),
    ...pathsFromInput(toolName, parsedInput, input.input),
    ...pathsFromCommand(nestedCommand),
  ]).slice(0, 12);
  const query = queryFromInput(parsedInput) || queryFromCommand(nestedCommand);
  const command = kind === "run_command" || kind === "test_app" || kind === "update_files"
    ? (nestedCommand || undefined)
    : undefined;

  return {
    kind,
    quietDetail: quietDetail(kind, files, query, command),
    files: files.length ? files : undefined,
    query: query || undefined,
    command: command || undefined,
    statusLine: status === "failed" || (input.exitCode != null && input.exitCode !== 0)
      ? (input.exitCode != null ? `Exit ${input.exitCode}` : "Failed")
      : undefined,
    result: result || undefined,
    developer: developerFor(toolName, input.input, transport, kind),
  };
}

export function cleanUserFacingOutput(value: string): string {
  return cleanOutput(value);
}

export function isTransportOnlyPayload(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed) return false;
  const json = parseJson(trimmed);
  if (json) {
    const keys = Object.keys(json);
    return keys.length > 0 && keys.every((k) => TRANSPORT_KEYS.has(k.toLowerCase()));
  }
  const lines = trimmed.split("\n").map((l) => l.trim()).filter(Boolean);
  return lines.length > 0 && lines.every((l) => isMetaLine(l) || l === "Output:");
}

export function extractTransportMetadata(input?: string, output?: string): Record<string, string> {
  return extractTransport(input, output);
}

function waitStatusLine(status: SemanticActionStatus | "running" | string, wall?: string): string {
  const dur = formatDuration(wall);
  if (status === "running") return dur ? `Waiting · ${dur}` : "Waiting";
  return dur ? `Finished · ${dur}` : "Finished";
}

function developerFor(
  toolName: string,
  rawInput: string | undefined,
  transport: Record<string, string>,
  kind: SemanticActionKind | "wait",
): ToolDeveloperDetails | undefined {
  // Read/search cards must not advertise exec_command as primary technical identity.
  if ((kind === "read_files" || kind === "search_code") && isExecLike(toolName)) {
    return Object.keys(transport).length
      ? { transport }
      : undefined;
  }
  const out: ToolDeveloperDetails = {};
  if (toolName && !isExecLike(toolName)) out.providerToolId = toolName;
  else if (toolName && kind !== "read_files" && kind !== "search_code") out.providerToolId = toolName;
  if (rawInput?.trim() && kind === "wait") out.rawInput = rawInput.trim().slice(0, 2000);
  if (Object.keys(transport).length) out.transport = transport;
  return out.providerToolId || out.rawInput || out.transport ? out : undefined;
}

function isExecLike(name: string): boolean {
  const n = name.toLowerCase();
  return n === "exec" || n === "exec_command" || n === "shell_command" || n.startsWith("multi:");
}


function commandFromNestedExec(toolName: string, raw?: string): string {
  if (!(isExecWrapperToolName(toolName) || toolName.startsWith("multi:"))) {
    return "";
  }
  const nested = parseExecWrapperCalls(raw);
  if (nested.length !== 1) {
    return "";
  }
  const call = nested[0];
  if (call.name !== "exec_command" && call.name !== "shell_command") {
    return "";
  }
  return str(call.object || {}, "cmd") || str(call.object || {}, "command");
}

function pathsFromInput(
  toolName: string,
  parsed: Record<string, unknown> | null,
  raw?: string,
): string[] {
  if (isExecWrapperToolName(toolName) || toolName.startsWith("multi:")) {
    return parseExecWrapperCalls(raw).flatMap((call) => {
      if (call.name === "exec_command" || call.name === "shell_command") {
        return pathsFromCommand(str(call.object || {}, "cmd") || str(call.object || {}, "command"));
      }
      return call.object ? pathsFromRecord(call.object) : [];
    });
  }
  return parsed ? pathsFromRecord(parsed) : [];
}

function pathsFromRecord(record: Record<string, unknown>): string[] {
  const out: string[] = [];
  for (const key of ["path", "file_path", "filePath", "filename", "file", "target_file", "targetFile"]) {
    const v = record[key];
    if (typeof v === "string" && looksPath(v)) out.push(v.trim());
  }
  for (const key of ["paths", "files", "file_paths"]) {
    const v = record[key];
    if (Array.isArray(v)) {
      for (const item of v) {
        if (typeof item === "string" && looksPath(item)) out.push(item.trim());
      }
    }
  }
  return out;
}

function pathsFromCommand(command?: string): string[] {
  if (!command?.trim()) return [];
  const tokens = tokenize(command);
  const exe = base(tokens[0] || "").toLowerCase();
  if (["cat", "sed", "nl", "less", "head", "tail"].includes(exe)) {
    return tokens.slice(1).filter((t) => looksPath(t) && !t.startsWith("-"));
  }
  if (exe === "ls") {
    const t = tokens.slice(1).find((x) => !x.startsWith("-"));
    return [t || "."];
  }
  return [];
}

function queryFromInput(parsed: Record<string, unknown> | null): string {
  if (!parsed) return "";
  return str(parsed, "pattern") || str(parsed, "query") || str(parsed, "glob_pattern") || str(parsed, "glob");
}

function queryFromCommand(command?: string): string {
  if (!command?.trim()) return "";
  const tokens = tokenize(command);
  const exe = base(tokens[0] || "").toLowerCase();
  if (!["rg", "grep", "ag", "ack"].includes(exe)) return "";
  let i = 1;
  while (i < tokens.length && tokens[i].startsWith("-")) {
    if (["-e", "--regexp"].includes(tokens[i]) && tokens[i + 1]) return tokens[i + 1];
    if (["-g", "-t", "--glob", "--type", "--max-count"].includes(tokens[i])) { i += 2; continue; }
    i += 1;
  }
  return tokens[i] || "";
}

function quietDetail(
  kind: SemanticActionKind | "wait",
  files: string[],
  query: string,
  command?: string,
): string | undefined {
  if (files.length === 1) return trunc(base(files[0]), 42);
  if (files.length > 1) return `${files.length} files`;
  if (query) return trunc(query, 42);
  if (kind === "run_command" && command) return trunc(command, 42);
  return undefined;
}

function classifyName(name: string): SemanticActionKind | "wait" {
  const l = name.toLowerCase();
  if (isWaitLikeToolName(l)) return "wait";
  if (l === "apply_patch" || l === "edit" || l === "write" || l === "multiedit") return "update_files";
  if (l === "grep" || l === "rg" || l.includes("search")) return "search_code";
  if (l === "read" || l === "read_file" || l === "glob" || l === "list_files" || l === "ls") return "read_files";
  if (l === "shell" || l === "bash" || l === "exec_command") return "run_command";
  return "use_tool";
}

function cleanOutput(value: string): string {
  if (!value?.trim()) return "";
  const trimmed = value.replace(/\r\n/g, "\n").trim();
  if (isTransportOnlyPayload(trimmed)) return "";
  const json = parseJson(trimmed);
  if (json) {
    const cleaned: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(json)) {
      if (!TRANSPORT_KEYS.has(k.toLowerCase())) cleaned[k] = v;
    }
    if (!Object.keys(cleaned).length) return "";
    if (typeof cleaned.output === "string") return cleanOutput(cleaned.output);
    if (typeof cleaned.content === "string") return cleanOutput(cleaned.content);
    try { return JSON.stringify(cleaned); } catch { return trimmed; }
  }
  const lines = trimmed.split("\n");
  const idx = lines.findIndex((l) => l.trim() === "Output:");
  const body = idx >= 0 ? lines.slice(idx + 1) : lines;
  return body.filter((l) => !isMetaLine(l)).join("\n").trim();
}

function extractTransport(input?: string, output?: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const source of [input, output]) {
    if (!source) continue;
    const json = parseJson(source);
    if (json) {
      for (const [k, v] of Object.entries(json)) {
        if (!TRANSPORT_KEYS.has(k.toLowerCase())) continue;
        const text = typeof v === "string" || typeof v === "number" ? String(v) : "";
        if (text) out[k.toLowerCase() === "wall_time_seconds" ? "wall_time" : k.toLowerCase()] = text;
      }
    }
    for (const line of source.split("\n")) {
      const t = line.trim();
      let m;
      if ((m = /^Chunk ID:\s*(.+)$/i.exec(t))) out.chunk_id = m[1].trim();
      if ((m = /^Wall time:\s*([0-9.]+)/i.exec(t))) out.wall_time = m[1].trim();
      if ((m = /^Process running with session ID\s+(\S+)/i.exec(t))) out.session_id = m[1].trim();
      if ((m = /^Original token count:\s*(\S+)/i.exec(t))) out.original_token_count = m[1].trim();
      if ((m = /^Process exited with code\s+(\S+)/i.exec(t)) || (m = /^Exit code:\s*(\S+)/i.exec(t))) {
        out.exit_code = m[1].trim();
      }
    }
  }
  return out;
}

function isMetaLine(line: string): boolean {
  const t = line.trim();
  return t.startsWith("Chunk ID:") || t.startsWith("Wall time:") || t.startsWith("Exit code:")
    || t.startsWith("Process exited with code ") || t.startsWith("Process running with session ID ")
    || t.startsWith("Original token count:") || t.startsWith("Total output lines:");
}

function normalizeStatus(status?: string, exitCode?: number): SemanticActionStatus {
  const n = (status || "").trim().toLowerCase();
  if (n === "running" || n === "in_progress") return "running";
  if (n === "blocked") return "blocked";
  if (n === "failed" || n === "error" || (exitCode != null && exitCode !== 0)) return "failed";
  return "done";
}

function formatDuration(value?: string): string | undefined {
  if (!value) return undefined;
  const seconds = Number(value);
  if (!Number.isFinite(seconds) || seconds < 0) return undefined;
  if (seconds < 10) return `${seconds.toFixed(seconds < 1 ? 2 : 1).replace(/\.0$/, "")}s`;
  if (seconds < 60) return `${Math.round(seconds)}s`;
  const m = Math.floor(seconds / 60);
  const r = Math.round(seconds % 60);
  return r > 0 ? `${m}m ${r}s` : `${m}m`;
}

function parseJson(value?: string): Record<string, unknown> | null {
  if (!value?.trim()) return null;
  try {
    const parsed = JSON.parse(value);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) return parsed as Record<string, unknown>;
  } catch { /* ignore */ }
  return null;
}

function cmdFrom(parsed: Record<string, unknown> | null): string {
  return parsed ? (str(parsed, "cmd") || str(parsed, "command")) : "";
}

function str(record: Record<string, unknown>, key: string): string {
  const v = record[key];
  return typeof v === "string" ? v.trim() : "";
}

function looksPath(value: string): boolean {
  const t = value.trim();
  if (!t || t.length > 400 || t.startsWith("{") || t.startsWith("[")) return false;
  return t.startsWith("/") || t.startsWith("./") || t.startsWith("../") || t.startsWith("~/")
    || t.includes("/") || /\.[A-Za-z0-9]{1,8}$/.test(t);
}

function unique(values: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const v of values) {
    const t = v.trim();
    if (!t || seen.has(t)) continue;
    seen.add(t);
    out.push(t);
  }
  return out;
}

function base(value: string): string {
  const parts = value.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] || value;
}

function trunc(value: string, limit: number): string {
  const chars = Array.from(value);
  return chars.length <= limit ? value : `${chars.slice(0, limit - 1).join("")}…`;
}

function tokenize(value: string): string[] {
  const tokens: string[] = [];
  let cur = "";
  let quote: "'" | '"' | "" = "";
  let esc = false;
  for (const ch of value) {
    if (esc) { cur += ch; esc = false; continue; }
    if (ch === "\\") { esc = true; continue; }
    if (quote) {
      if (ch === quote) quote = "";
      else cur += ch;
      continue;
    }
    if (ch === "'" || ch === '"') { quote = ch; continue; }
    if (/\s/.test(ch)) {
      if (cur) { tokens.push(cur); cur = ""; }
      continue;
    }
    cur += ch;
  }
  if (cur) tokens.push(cur);
  return tokens;
}
