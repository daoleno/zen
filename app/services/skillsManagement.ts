export type SkillAgent =
  | "codex"
  | "claude-code"
  | "cursor"
  | "grok"
  | "opencode"
  | "pi";
export type ManagedSkillAgent = SkillAgent;
export type SkillScope =
  | "project"
  | "global"
  | "plugin"
  | "builtin"
  | "unknown";
export type SkillMutationOperation = "delete";

export interface SkillRiskSignal {
  type: string;
  detail?: string;
  severity: "info" | "warn" | "alert";
  file?: string;
}

export interface SkillAgentSupport {
  agent: SkillAgent;
  name: string;
  supported: boolean;
  globalScope: boolean;
  projectScope: boolean;
  defaultGlobalDir: string;
  reason?: string;
}

export interface ExecutorSupport {
  name: string;
  kind?: string;
  agent: SkillAgent;
  command?: string;
}

export type SkillDeleteCapability =
  | { canDelete: true; reason?: string }
  | { canDelete: false; reason?: string };

export interface InstalledSkill {
  id: string;
  name: string;
  description?: string;
  enabled: boolean;
  rootPath: string;
  canonicalPath: string;
  allowedRoot: string;
  location: string;
  scope: SkillScope;
  agents: SkillAgent[];
  contentHash?: string;
  plugin?: string;
  risk?: SkillRiskSignal[];
  warnings?: string[];
  capability: SkillDeleteCapability;
}

export interface SkillsInventory {
  generatedAt: string;
  cwd?: string;
  skills: InstalledSkill[];
  agents: SkillAgentSupport[];
  executors?: ExecutorSupport[];
  warnings: string[];
  mutationOperations: SkillMutationOperation[];
}

export interface SkillsMutationCommand {
  operation: "delete";
  copyId: string;
  skillName: string;
  rootPath: string;
  canonicalPath: string;
  allowedRoot: string;
  location: string;
  scope: SkillScope;
  agents: SkillAgent[];
  summary: string;
  destructive: true;
}

export interface SkillsMutationExecution {
  success: boolean;
  exitCode: number;
  output: string;
  durationMs: number;
}

export interface SkillsMutationResult {
  command: SkillsMutationCommand;
  execution: SkillsMutationExecution;
}

export interface PackageFile {
  path: string;
  size: number;
  mode: string;
  kind: "markdown" | "json" | "text" | "binary";
  mediaType: string;
  previewStatus: "ready" | "large" | "binary";
}

export interface FilePreview {
  path: string;
  kind: PackageFile["kind"];
  mediaType: string;
  status: "ready" | "truncated" | "binary";
  size: number;
  bytesReturned: number;
  content?: string;
  notice?: string;
}

export interface PackageDetail {
  copyId: string;
  skillName: string;
  description?: string;
  enabled: boolean;
  rootPath: string;
  canonicalPath: string;
  allowedRoot: string;
  location: string;
  contentHash?: string;
  scope: SkillScope;
  agents: SkillAgent[];
  files?: PackageFile[];
  preview?: FilePreview;
  risk?: SkillRiskSignal[];
  warnings?: string[];
  capability: SkillDeleteCapability;
}

export type SkillsRequestState<T> =
  | { status: "idle"; generation: number; data?: undefined; error?: undefined }
  | { status: "loading"; generation: number; data?: T; error?: undefined }
  | { status: "ready"; generation: number; data: T; error?: undefined }
  | { status: "empty"; generation: number; data: T; error?: undefined }
  | { status: "error"; generation: number; data?: T; error: string };

export interface SkillDeleteIdentity {
  operation: "delete";
  skillId: string;
  skillName: string;
  rootPath: string;
  canonicalPath: string;
  allowedRoot: string;
  cwd?: string;
}

const AGENTS = new Set<SkillAgent>([
  "codex",
  "claude-code",
  "cursor",
  "grok",
  "opencode",
  "pi",
]);
const SCOPES = new Set<SkillScope>([
  "project",
  "global",
  "plugin",
  "builtin",
  "unknown",
]);
const INSTALLED_ID_PATTERN = /^[a-f0-9]{24}$/;
const SKILL_PATTERN = /^[a-z0-9](?:[a-z0-9._-]{0,126}[a-z0-9])?$/;
const MAX_INVENTORY_SKILLS = 600;
const MAX_FILES = 512;

export function normalizeSkillsInventory(value: unknown): SkillsInventory {
  const raw = record(value);
  const rawSkills = Array.isArray(raw.skills) ? raw.skills : [];
  if (rawSkills.length > MAX_INVENTORY_SKILLS) {
    throw new Error("Daemon returned too many installed Skills.");
  }
  const skills = rawSkills
    .map(normalizeInstalledSkill)
    .filter((skill): skill is InstalledSkill => skill != null);
  if (
    skills.length !== rawSkills.length ||
    new Set(skills.map((skill) => skill.id)).size !== skills.length ||
    new Set(skills.map((skill) => skill.rootPath)).size !== skills.length
  ) {
    throw new Error("Daemon returned an invalid Skills inventory.");
  }
  const generatedAt = boundedString(raw.generated_at, 64);
  if (!generatedAt || Number.isNaN(Date.parse(generatedAt))) {
    throw new Error("Daemon returned an invalid Skills inventory timestamp.");
  }
  const rawAgents = Array.isArray(raw.agents) ? raw.agents : [];
  const agents = rawAgents
    .map(normalizeAgentSupport)
    .filter((agent): agent is SkillAgentSupport => agent != null);
  if (
    rawAgents.length > AGENTS.size ||
    agents.length !== rawAgents.length ||
    new Set(agents.map((agent) => agent.agent)).size !== agents.length
  ) {
    throw new Error("Daemon returned an invalid Skill agent contract.");
  }
  const rawExecutors = Array.isArray(raw.executors) ? raw.executors : [];
  const executors = rawExecutors
    .map(normalizeExecutorSupport)
    .filter((executor): executor is ExecutorSupport => executor != null);
  if (executors.length !== rawExecutors.length) {
    throw new Error("Daemon returned an invalid executor contract.");
  }
  const operations = normalizeMutationOperations(raw.mutation_operations);
  return {
    generatedAt,
    cwd: boundedString(raw.cwd, 4096) || undefined,
    skills,
    agents,
    executors: executors.length ? executors : undefined,
    warnings: normalizeWarnings(raw.warnings),
    mutationOperations: operations,
  };
}

function normalizeInstalledSkill(value: unknown): InstalledSkill | null {
  const raw = record(value);
  const id = boundedString(raw.id, 24);
  const name = boundedString(raw.name, 128);
  const rootPath = boundedString(raw.root_path, 4096);
  const canonicalPath = boundedString(raw.canonical_path, 4096);
  const allowedRoot = boundedString(raw.allowed_root, 4096);
  const location = boundedString(raw.location, 240);
  const scope = raw.scope;
  const agents = normalizeAgents(raw.agents);
  if (
    !INSTALLED_ID_PATTERN.test(id) ||
    !isSkillName(name) ||
    !rootPath ||
    !canonicalPath ||
    !allowedRoot ||
    !location ||
    typeof scope !== "string" ||
    !SCOPES.has(scope as SkillScope) ||
    agents == null ||
    typeof raw.enabled !== "boolean"
  ) {
    return null;
  }
  return {
    id,
    name,
    description: boundedString(raw.description, 240) || undefined,
    enabled: raw.enabled,
    rootPath,
    canonicalPath,
    allowedRoot,
    location,
    scope: scope as SkillScope,
    agents,
    contentHash: boundedString(raw.content_hash, 64) || undefined,
    plugin: boundedString(raw.plugin, 128) || undefined,
    risk: normalizeRisks(raw.risk),
    warnings: normalizeWarnings(raw.warnings),
    capability: normalizeDeleteCapability(raw.capability),
  };
}

export function normalizeSkillsMutationCommand(
  value: unknown,
): SkillsMutationCommand {
  const raw = record(value);
  const command = {
    operation: raw.operation,
    copyId: boundedString(raw.copy_id, 24),
    skillName: boundedString(raw.skill_name, 128),
    rootPath: boundedString(raw.root_path, 4096),
    canonicalPath: boundedString(raw.canonical_path, 4096),
    allowedRoot: boundedString(raw.allowed_root, 4096),
    location: boundedString(raw.location, 240),
    scope: raw.scope,
    agents: normalizeAgents(raw.agents),
    summary: boundedString(raw.summary, 512),
    destructive: raw.destructive,
  };
  if (
    command.operation !== "delete" ||
    !INSTALLED_ID_PATTERN.test(command.copyId) ||
    !isSkillName(command.skillName) ||
    !command.rootPath ||
    !command.canonicalPath ||
    !command.allowedRoot ||
    !command.location ||
    typeof command.scope !== "string" ||
    !SCOPES.has(command.scope as SkillScope) ||
    command.agents == null ||
    !command.summary ||
    command.destructive !== true
  ) {
    throw new Error("Daemon returned an invalid Skills delete command.");
  }
  return command as SkillsMutationCommand;
}

export function normalizeSkillsMutationResult(
  value: unknown,
): SkillsMutationResult {
  const raw = record(value);
  const command = normalizeSkillsMutationCommand(raw.command);
  const success = raw.success;
  const exitCode = raw.exit_code;
  const durationMs = raw.duration_ms;
  if (
    typeof success !== "boolean" ||
    !Number.isSafeInteger(exitCode) ||
    (exitCode as number) < -1 ||
    (exitCode as number) > 255 ||
    !Number.isSafeInteger(durationMs) ||
    (durationMs as number) < 0 ||
    (durationMs as number) > 3_600_000 ||
    success !== (exitCode === 0)
  ) {
    throw new Error("Daemon returned inconsistent Skills deletion state.");
  }
  return {
    command,
    execution: {
      success,
      exitCode: exitCode as number,
      output: boundedString(raw.output, 60_000),
      durationMs: durationMs as number,
    },
  };
}

export function assertSkillsMutationMatchesRequest(
  result: SkillsMutationResult,
  expected: SkillDeleteIdentity,
): void {
  assertSkillsCommandMatchesRequest(result.command, expected);
}

export function assertSkillsCommandMatchesRequest(
  command: SkillsMutationCommand,
  expected: SkillDeleteIdentity,
): void {
  if (
    command.operation !== expected.operation ||
    command.copyId !== expected.skillId ||
    command.skillName !== expected.skillName ||
    command.rootPath !== expected.rootPath ||
    command.canonicalPath !== expected.canonicalPath ||
    command.allowedRoot !== expected.allowedRoot
  ) {
    throw new Error("Daemon returned a Skills command for a different copy.");
  }
}

export function normalizeSkillsInspectDetail(value: unknown): PackageDetail {
  const raw = record(value);
  const base = normalizeInstalledSkill({
    id: raw.copy_id,
    name: raw.skill_name,
    description: raw.description,
    enabled: raw.enabled,
    root_path: raw.root_path,
    canonical_path: raw.canonical_path,
    allowed_root: raw.allowed_root,
    location: raw.location,
    scope: raw.scope,
    agents: raw.agents,
    content_hash: raw.content_hash,
    risk: raw.risk,
    warnings: raw.warnings,
    capability: raw.capability,
  });
  if (!base) {
    throw new Error("Daemon returned an invalid Skills detail.");
  }
  const rawFiles = Array.isArray(raw.files) ? raw.files : [];
  if (rawFiles.length > MAX_FILES) {
    throw new Error("Daemon returned too many Skill package files.");
  }
  const files = rawFiles
    .map(normalizePackageFile)
    .filter((file): file is PackageFile => file != null);
  if (
    files.length !== rawFiles.length ||
    new Set(files.map((file) => file.path)).size !== files.length
  ) {
    throw new Error("Daemon returned an invalid Skill package file list.");
  }
  const preview = normalizeFilePreview(raw.preview);
  if (preview && !files.some((file) => file.path === preview.path)) {
    throw new Error("Daemon returned a preview outside the Skill file list.");
  }
  return {
    copyId: base.id,
    skillName: base.name,
    description: base.description,
    enabled: base.enabled,
    rootPath: base.rootPath,
    canonicalPath: base.canonicalPath,
    allowedRoot: base.allowedRoot,
    location: base.location,
    contentHash: base.contentHash,
    scope: base.scope,
    agents: base.agents,
    files: files.length ? files : undefined,
    preview,
    risk: base.risk,
    warnings: base.warnings,
    capability: base.capability,
  };
}

function normalizePackageFile(value: unknown): PackageFile | null {
  const raw = record(value);
  const path = boundedString(raw.path, 1024);
  const kind = raw.kind;
  const mediaType = boundedString(raw.media_type, 128);
  const previewStatus = raw.preview_status;
  if (
    !isSafeSkillFilePath(path) ||
    !Number.isSafeInteger(raw.size) ||
    (raw.size as number) < 0 ||
    !boundedString(raw.mode, 16) ||
    (kind !== "markdown" &&
      kind !== "json" &&
      kind !== "text" &&
      kind !== "binary") ||
    !mediaType ||
    (previewStatus !== "ready" &&
      previewStatus !== "large" &&
      previewStatus !== "binary")
  ) {
    return null;
  }
  return {
    path,
    size: raw.size as number,
    mode: raw.mode as string,
    kind,
    mediaType,
    previewStatus,
  };
}

function normalizeFilePreview(value: unknown): FilePreview | undefined {
  if (value == null) return undefined;
  const raw = record(value);
  const path = boundedString(raw.path, 1024);
  const kind = raw.kind;
  const mediaType = boundedString(raw.media_type, 128);
  const status = raw.status;
  if (
    !isSafeSkillFilePath(path) ||
    (kind !== "markdown" && kind !== "json" && kind !== "text" && kind !== "binary") ||
    !mediaType ||
    (status !== "ready" && status !== "truncated" && status !== "binary") ||
    !Number.isSafeInteger(raw.size) ||
    (raw.size as number) < 0 ||
    !Number.isSafeInteger(raw.bytes_returned) ||
    (raw.bytes_returned as number) < 0
  ) {
    throw new Error("Daemon returned an invalid Skill file preview.");
  }
  return {
    path,
    kind,
    mediaType,
    status,
    size: raw.size as number,
    bytesReturned: raw.bytes_returned as number,
    content: boundedMultilineString(raw.content, 70_000) || undefined,
    notice: boundedString(raw.notice, 240) || undefined,
  };
}

function normalizeDeleteCapability(value: unknown): SkillDeleteCapability {
  const raw = record(value);
  return raw.can_delete === true
    ? { canDelete: true, reason: boundedString(raw.reason, 240) || undefined }
    : { canDelete: false, reason: boundedString(raw.reason, 240) || undefined };
}

function normalizeMutationOperations(value: unknown): SkillMutationOperation[] {
  if (!Array.isArray(value) || value.length !== 1 || value[0] !== "delete") {
    throw new Error("Daemon returned an invalid Skills mutation capability.");
  }
  return ["delete"];
}

function normalizeAgentSupport(value: unknown): SkillAgentSupport | null {
  const raw = record(value);
  const agent = raw.agent;
  const name = boundedString(raw.name, 40);
  const defaultGlobalDir = boundedString(raw.default_global_dir, 4096);
  if (
    typeof agent !== "string" ||
    !AGENTS.has(agent as SkillAgent) ||
    !name ||
    !defaultGlobalDir
  ) {
    return null;
  }
  return {
    agent: agent as SkillAgent,
    name,
    supported: raw.supported === true,
    globalScope: raw.global_scope === true,
    projectScope: raw.project_scope === true,
    defaultGlobalDir,
    reason: boundedString(raw.reason, 240) || undefined,
  };
}

function normalizeExecutorSupport(value: unknown): ExecutorSupport | null {
  const raw = record(value);
  const name = boundedString(raw.name, 128);
  const agent = raw.agent;
  if (!name || typeof agent !== "string" || !AGENTS.has(agent as SkillAgent)) {
    return null;
  }
  return {
    name,
    kind: boundedString(raw.kind, 40) || undefined,
    agent: agent as SkillAgent,
    command: boundedString(raw.command, 1024) || undefined,
  };
}

function normalizeAgents(value: unknown): SkillAgent[] | null {
  if (value == null) return [];
  if (!Array.isArray(value) || value.length > AGENTS.size) return null;
  const seen = new Set<SkillAgent>();
  for (const raw of value) {
    if (typeof raw !== "string" || !AGENTS.has(raw as SkillAgent)) return null;
    seen.add(raw as SkillAgent);
  }
  return [...seen];
}

function normalizeRisks(value: unknown): SkillRiskSignal[] | undefined {
  const risks = (Array.isArray(value) ? value : [])
    .map((candidate): SkillRiskSignal | null => {
      const raw = record(candidate);
      const type = boundedString(raw.type, 40);
      const severity = raw.severity;
      if (!type || (severity !== "info" && severity !== "warn" && severity !== "alert")) {
        return null;
      }
      return {
        type,
        severity,
        detail: boundedString(raw.detail, 240) || undefined,
        file: boundedString(raw.file, 1024) || undefined,
      };
    })
    .filter((risk): risk is SkillRiskSignal => risk != null);
  return risks.length ? risks : undefined;
}

function normalizeWarnings(value: unknown): string[] {
  return (Array.isArray(value) ? value : [])
    .map((warning) => boundedString(warning, 240))
    .filter(Boolean)
    .slice(0, 12);
}

function isSafeSkillFilePath(path: string): boolean {
  if (!path || path.startsWith("/") || path.includes("\\")) return false;
  return path.split("/").every((part) => part !== "" && part !== "." && part !== "..");
}

export function createSkillsRequestState<T>(): SkillsRequestState<T> {
  return { status: "idle", generation: 0 };
}

export function beginSkillsRequest<T>(
  current: SkillsRequestState<T>,
  generation = current.generation + 1,
  preserveData = true,
): SkillsRequestState<T> {
  if (generation <= current.generation) return current;
  const data = preserveData ? current.data : undefined;
  return data === undefined
    ? { status: "loading", generation }
    : { status: "loading", generation, data };
}

export function skillsRequestData<T>(state: SkillsRequestState<T>): T | undefined {
  return state.data;
}

export function completeSkillsRequest<T>(
  current: SkillsRequestState<T>,
  generation: number,
  data: T,
  empty: boolean,
): SkillsRequestState<T> {
  if (current.generation !== generation || current.status !== "loading") return current;
  return empty
    ? { status: "empty", generation, data }
    : { status: "ready", generation, data };
}

export function failSkillsRequest<T>(
  current: SkillsRequestState<T>,
  generation: number,
  error: string,
  preserveData = true,
): SkillsRequestState<T> {
  if (current.generation !== generation || current.status !== "loading") return current;
  const data = preserveData ? current.data : undefined;
  const message = error.trim() || "Request failed.";
  return data === undefined
    ? { status: "error", generation, error: message }
    : { status: "error", generation, data, error: message };
}

export function buildSkillsMutationConfirmation(command: SkillsMutationCommand): {
  title: string;
  message: string;
  confirmLabel: string;
} {
  const availableTo = command.agents.length
    ? command.agents.map(skillAgentLabel).join(", ")
    : "no active Agent";
  return {
    title: `Delete ${command.skillName}?`,
    message: [
      `This permanently deletes "${command.skillName}".`,
      `Available to: ${availableTo}`,
      `Location: ${command.location}`,
      "",
      "This action cannot be undone.",
    ].join("\n"),
    confirmLabel: "Delete",
  };
}

export function skillAgentLabel(agent: SkillAgent): string {
  switch (agent) {
    case "codex": return "Codex";
    case "claude-code": return "Claude Code";
    case "cursor": return "Cursor";
    case "grok": return "Grok";
    case "opencode": return "OpenCode";
    case "pi": return "Pi";
  }
}

export function scopeLabel(scope: SkillScope): string {
  switch (scope) {
    case "project": return "Project";
    case "global": return "Global";
    case "plugin": return "Plugin";
    case "builtin": return "Built in";
    default: return "Local";
  }
}

export function isSkillName(value: string): boolean {
  return value !== "." && value !== ".." && SKILL_PATTERN.test(value);
}

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function boundedString(value: unknown, maxLength: number): string {
  if (
    typeof value !== "string" ||
    value.length > maxLength ||
    /[\u0000-\u001f\u007f]/.test(value)
  ) return "";
  return value;
}

function boundedMultilineString(value: unknown, maxLength: number): string {
  if (
    typeof value !== "string" ||
    value.length > maxLength ||
    /[\u0000\u0001-\u0008\u000b\u000c\u000e-\u001f\u007f]/.test(value)
  ) return "";
  return value;
}
