export type SkillAgent =
  "codex" | "claude-code" | "cursor" | "grok" | "opencode" | "pi";
/** Every Zen-supported Agent now has a real adapter; all six are managed. */
export type ManagedSkillAgent = SkillAgent;
export type SkillScope =
  "project" | "global" | "mixed" | "plugin" | "builtin" | "unknown";
export type SkillManager =
  "zen" | "external" | "plugin" | "builtin" | "unknown";
export type BindingMode = "symlink" | "copy";
export type SkillMutationOperation =
  | "import"
  | "migrate"
  | "bind"
  | "unbind"
  | "enable"
  | "disable"
  | "uninstall"
  | "forget"
  | "adopt"
  | "update";

export interface SkillBinding {
  agent: SkillAgent;
  scope: SkillScope;
  mode: BindingMode;
  targetPath: string;
  sourcePath: string;
  enabled: boolean;
  boundAt?: string;
  driftHash?: string;
  note?: string;
  operations: SkillMutationOperation[];
}

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
  bindingMode: BindingMode;
  bindingModeNote?: string;
  defaultGlobalDir: string;
  reason?: string;
}

export interface ExecutorSupport {
  name: string;
  kind?: string;
  agent: SkillAgent;
  command?: string;
}

export type SkillManagementCapability =
  | {
      canManage: true;
      operations: SkillMutationOperation[];
      reason?: string;
    }
  | {
      canManage: false;
      operations: [];
      reason?: string;
    };

export interface InstalledSkill {
  id: string;
  name: string;
  description?: string;
  manager: SkillManager;
  owned: boolean;
  tracked: boolean;
  enabled: boolean;
  canonicalPath: string;
  sourcePath: string;
  scope: SkillScope;
  agents: SkillAgent[];
  bindings: SkillBinding[];
  provenance: string;
  source?: string;
  sourceType?: string;
  sourceUrl?: string;
  ref?: string;
  contentHash?: string;
  installedAt?: string;
  updatedAt?: string;
  plugin?: string;
  risk?: SkillRiskSignal[];
  warnings?: string[];
  migration?: "owned" | "external" | "duplicate" | "conflict" | string;
  capability: SkillManagementCapability;
}

export interface SkillsInventory {
  generatedAt: string;
  cwd?: string;
  skills: InstalledSkill[];
  agents: SkillAgentSupport[];
  executors?: ExecutorSupport[];
  warnings: string[];
  mutationOperations: SkillMutationOperation[];
  migration: {
    owned: number;
    external: number;
    duplicate: number;
    conflict: number;
    tracked: number;
  };
}

export interface CatalogSkill {
  id: string;
  skillId: string;
  name: string;
  installs: number;
  source: string;
  installable: boolean;
}

export interface SkillsCatalogResult {
  query: string;
  skills: CatalogSkill[];
}

export type SkillsLeaderboardView = "all-time" | "trending" | "hot";

export interface RankedCatalogSkill {
  id: string;
  skillId: string;
  name: string;
  source: string;
  rank: number;
  totalInstalls?: number;
  installs24h?: number;
  currentInstalls?: number;
  yesterdayInstalls?: number;
  change?: number;
  installable: boolean;
}

export interface SkillsLeaderboard {
  view: SkillsLeaderboardView;
  totalSkills: number;
  skills: RankedCatalogSkill[];
  warning?: string;
}

export interface SkillsLeaderboards {
  allTime: SkillsLeaderboard;
  trending: SkillsLeaderboard;
  hot: SkillsLeaderboard;
}

export interface SkillMutationChange {
  kind: "create_dir" | "copy_file" | "symlink" | "remove" | "keep" | "write";
  path: string;
  destination?: string;
  detail?: string;
}

export interface SkillsMutationCommand {
  operation: SkillMutationOperation;
  scope: "project" | "global";
  agents: SkillAgent[];
  skillName: string;
  catalogId?: string;
  source?: string;
  ref?: string;
  summary: string;
  changes: SkillMutationChange[];
  destructive: boolean;
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
}

export interface PackageDetail {
  skillName: string;
  description?: string;
  manager: SkillManager;
  owned: boolean;
  tracked: boolean;
  enabled: boolean;
  canonicalPath?: string;
  sourcePath?: string;
  source?: string;
  sourceType?: string;
  sourceUrl?: string;
  ref?: string;
  contentHash?: string;
  installedAt?: string;
  updatedAt?: string;
  scope: SkillScope;
  agents: SkillAgent[];
  bindings: SkillBinding[];
  files?: PackageFile[];
  skillMd?: string;
  filePath?: string;
  fileContent?: string;
  risk?: SkillRiskSignal[];
  warnings?: string[];
  capability: SkillManagementCapability;
}

export type SkillsRequestState<T> =
  | { status: "idle"; generation: number; data?: undefined; error?: undefined }
  | {
      status: "loading";
      generation: number;
      data?: T;
      error?: undefined;
    }
  | { status: "ready"; generation: number; data: T; error?: undefined }
  | { status: "empty"; generation: number; data: T; error?: undefined }
  | { status: "error"; generation: number; data?: T; error: string };

const AGENTS = new Set<SkillAgent>([
  "codex",
  "claude-code",
  "cursor",
  "grok",
  "opencode",
  "pi",
]);
const MANAGED_AGENTS = AGENTS;
const SCOPES = new Set<SkillScope>([
  "project",
  "global",
  "mixed",
  "plugin",
  "builtin",
  "unknown",
]);
const MANAGERS = new Set<SkillManager>([
  "zen",
  "external",
  "plugin",
  "builtin",
  "unknown",
]);
const OPERATIONS = new Set<SkillMutationOperation>([
  "import",
  "migrate",
  "bind",
  "unbind",
  "enable",
  "disable",
  "uninstall",
  "forget",
  "adopt",
  "update",
]);
const BINDING_MODES = new Set<BindingMode>(["symlink", "copy"]);
const CHANGE_KINDS = new Set<string>([
  "create_dir",
  "copy_file",
  "symlink",
  "remove",
  "keep",
  "write",
]);
const LEGACY_MUTATION_OPERATIONS: SkillMutationOperation[] = [];
const SKILL_PATTERN = /^[a-z0-9](?:[a-z0-9._-]{0,126}[a-z0-9])?$/;
const OWNER_PATTERN = /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$/;
const REPO_PATTERN = /^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,98}[A-Za-z0-9])?$/;
const INSTALLED_ID_PATTERN = /^[a-f0-9]{24}$/;
const MAX_INVENTORY_SKILLS = 600;
const MAX_CATALOG_SKILLS = 30;
const MAX_LEADERBOARD_SKILLS = 30;
const MAX_LEADERBOARD_TOTAL = 10_000_000;
const MAX_CATALOG_METRIC = 1_000_000_000_000;
const MAX_BINDINGS = 12;
const MAX_CHANGES = 24;
const MAX_FILES = 128;

export function normalizeSkillsInventory(value: unknown): SkillsInventory {
  const inventory = record(value);
  const rawSkills = Array.isArray(inventory.skills) ? inventory.skills : [];
  if (rawSkills.length > MAX_INVENTORY_SKILLS) {
    throw new Error("Daemon returned too many installed Skills.");
  }
  const skills: InstalledSkill[] = [];
  let skippedSkills = 0;
  const installedIDs = new Set<string>();
  const canonicalPaths = new Set<string>();
  for (const rawSkill of rawSkills) {
    const skill = normalizeInstalledSkill(rawSkill);
    if (!skill) {
      skippedSkills += 1;
      continue;
    }
    if (installedIDs.has(skill.id) || canonicalPaths.has(skill.canonicalPath)) {
      skippedSkills += 1;
      continue;
    }
    installedIDs.add(skill.id);
    canonicalPaths.add(skill.canonicalPath);
    skills.push(skill);
  }
  const rawAgents = Array.isArray(inventory.agents) ? inventory.agents : [];
  if (rawAgents.length > AGENTS.size) {
    throw new Error("Daemon returned too many Skill agent contracts.");
  }
  const agents = rawAgents
    .map(normalizeAgentSupport)
    .filter((agent): agent is SkillAgentSupport => agent != null);
  if (
    agents.length !== rawAgents.length ||
    new Set(agents.map((agent) => agent.agent)).size !== agents.length
  ) {
    throw new Error("Daemon returned an invalid Skill agent contract.");
  }
  const generatedAt = boundedString(inventory.generated_at, 64);
  if (!generatedAt || Number.isNaN(Date.parse(generatedAt))) {
    throw new Error("Daemon returned an invalid Skills inventory timestamp.");
  }
  const rawExecutors = Array.isArray(inventory.executors)
    ? inventory.executors
    : [];
  const executors = rawExecutors
    .map(normalizeExecutorSupport)
    .filter((executor): executor is ExecutorSupport => executor != null);
  if (executors.length !== rawExecutors.length) {
    throw new Error("Daemon returned an invalid executor contract.");
  }
  const migrationRaw = record(inventory.migration);
  const migration = {
    owned: boundedCount(migrationRaw.owned),
    external: boundedCount(migrationRaw.external),
    duplicate: boundedCount(migrationRaw.duplicate),
    conflict: boundedCount(migrationRaw.conflict),
    tracked: boundedCount(migrationRaw.tracked),
  };
  const warnings = (Array.isArray(inventory.warnings) ? inventory.warnings : [])
    .map((warning) => boundedString(warning, 240))
    .filter(Boolean)
    .slice(0, 12);
  if (skippedSkills > 0 && warnings.length < 12) {
    warnings.push(
      `${skippedSkills} installed Skill ${skippedSkills === 1 ? "entry was" : "entries were"} unreadable and skipped.`,
    );
  }
  return {
    generatedAt,
    cwd: boundedString(inventory.cwd, 4096) || undefined,
    skills,
    agents,
    executors: executors.length > 0 ? executors : undefined,
    warnings,
    mutationOperations: normalizeMutationOperations(
      inventory.mutation_operations,
    ),
    migration,
  };
}

function boundedCount(value: unknown): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) {
    return 0;
  }
  return value;
}

/**
 * The daemon's authoritative mutation capability list. The App gates every
 * lifecycle affordance on this list; an absent field is rejected so an old
 * daemon can never silently disable every action.
 */
function normalizeMutationOperations(value: unknown): SkillMutationOperation[] {
  if (
    !Array.isArray(value) ||
    value.length < 1 ||
    value.length > OPERATIONS.size
  ) {
    throw new Error("Daemon returned an invalid Skills mutation capability.");
  }
  const seen = new Set<SkillMutationOperation>();
  for (const raw of value) {
    if (
      typeof raw !== "string" ||
      !OPERATIONS.has(raw as SkillMutationOperation) ||
      seen.has(raw as SkillMutationOperation)
    ) {
      throw new Error("Daemon returned an invalid Skills mutation capability.");
    }
    seen.add(raw as SkillMutationOperation);
  }
  return [...seen];
}

export function normalizeSkillsCatalogResult(
  value: unknown,
): SkillsCatalogResult {
  const result = record(value);
  const query = boundedString(result.query, 80).trim();
  const rawSkills = Array.isArray(result.skills) ? result.skills : [];
  if (rawSkills.length > MAX_CATALOG_SKILLS) {
    throw new Error("Daemon returned too many catalog Skills.");
  }
  const skills: CatalogSkill[] = [];
  const identities = new Set<string>();
  for (const raw of rawSkills) {
    const candidate = record(raw);
    const id = boundedString(candidate.id, 272);
    const name = boundedString(candidate.name, 128);
    const source = boundedString(candidate.source, 141);
    const installs = candidate.installs;
    if (
      !isCatalogIdentity(id, source, name) ||
      typeof installs !== "number" ||
      !Number.isSafeInteger(installs) ||
      installs < 0 ||
      installs > 1_000_000_000_000 ||
      identities.has(id)
    ) {
      continue;
    }
    identities.add(id);
    skills.push({
      id,
      skillId: name,
      name,
      source,
      installs,
      installable: true,
    });
  }
  return { query, skills };
}

export function normalizeSkillsLeaderboards(
  value: unknown,
): SkillsLeaderboards {
  const raw = exactRecord(value, ["all_time", "trending", "hot"]);
  return {
    allTime: normalizeSkillsLeaderboard(raw.all_time, "all-time"),
    trending: normalizeSkillsLeaderboard(raw.trending, "trending"),
    hot: normalizeSkillsLeaderboard(raw.hot, "hot"),
  };
}

export function normalizeSkillsMutationCommand(
  value: unknown,
): SkillsMutationCommand {
  const raw = record(value);
  const operation = raw.operation;
  const scope = raw.scope;
  const skillName = boundedString(raw.skill_name, 128);
  const summary = boundedString(raw.summary, 512);
  const destructive = raw.destructive === true;
  if (
    typeof operation !== "string" ||
    !OPERATIONS.has(operation as SkillMutationOperation) ||
    (scope !== "project" && scope !== "global") ||
    !summary
  ) {
    throw new Error("Daemon returned an invalid Skills command.");
  }
  const agents = normalizeAgents(raw.agents);
  if (agents == null) {
    throw new Error("Daemon returned invalid Skill targets.");
  }
  const rawChanges = Array.isArray(raw.changes) ? raw.changes : [];
  if (rawChanges.length < 1 || rawChanges.length > MAX_CHANGES) {
    throw new Error("Daemon returned an invalid Skills change plan.");
  }
  const changes: SkillMutationChange[] = [];
  for (const change of rawChanges) {
    const candidate = record(change);
    const kind = candidate.kind;
    const path = boundedString(candidate.path, 4096);
    const destination = boundedString(candidate.destination, 4096);
    const detail = boundedString(candidate.detail, 240);
    if (
      typeof kind !== "string" ||
      !CHANGE_KINDS.has(kind) ||
      !path ||
      (candidate.destination != null && !destination)
    ) {
      throw new Error("Daemon returned an invalid Skills change.");
    }
    changes.push({
      kind: kind as SkillMutationChange["kind"],
      path,
      destination: destination || undefined,
      detail: detail || undefined,
    });
  }
  const normalized: SkillsMutationCommand = {
    operation: operation as SkillMutationOperation,
    scope,
    skillName,
    agents,
    summary,
    changes,
    destructive,
  };
  if (operation === "import") {
    const catalogId = boundedString(raw.catalog_id, 272);
    const source = boundedString(raw.source, 1024);
    const ref = boundedString(raw.ref, 128);
    if (!catalogId || !source) {
      throw new Error("Daemon returned an unbound Skills import command.");
    }
    if (
      !isRepository(source) ||
      !isSkillName(skillName) ||
      catalogId !== `${source}/${skillName}`
    ) {
      throw new Error("Daemon returned an invalid Skills import command.");
    }
    normalized.catalogId = catalogId;
    normalized.source = source;
    normalized.ref = ref || undefined;
  } else if (operation !== "migrate") {
    if (!isSkillName(skillName)) {
      throw new Error("Daemon returned an invalid Skills command.");
    }
    const source = boundedString(raw.source, 1024);
    const ref = boundedString(raw.ref, 128);
    if (source) {
      normalized.source = source;
    }
    if (ref) {
      normalized.ref = ref;
    }
  }
  if (operation === "migrate" && skillName) {
    throw new Error("Daemon returned an invalid Skills migrate command.");
  }
  if (!isValidMutationCommand(normalized)) {
    throw new Error("Daemon returned a non-reviewable Skills command.");
  }
  return normalized;
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
    typeof exitCode !== "number" ||
    !Number.isSafeInteger(exitCode) ||
    exitCode < -1 ||
    exitCode > 255 ||
    typeof durationMs !== "number" ||
    !Number.isSafeInteger(durationMs) ||
    durationMs < 0 ||
    durationMs > 3600_000
  ) {
    throw new Error("Daemon returned an invalid Skills mutation outcome.");
  }
  const output = boundedString(raw.output, 60000);
  if (success !== (exitCode === 0)) {
    throw new Error("Daemon returned inconsistent Skills mutation state.");
  }
  return {
    command,
    execution: { success, exitCode, output, durationMs },
  };
}

/** Asserts the daemon's executed command matches the exact reviewed request. */
export function assertSkillsMutationMatchesRequest(
  result: SkillsMutationResult,
  expected: {
    operation: SkillMutationOperation;
    skillId?: string;
    source?: string;
    skillName?: string;
    scope: "project" | "global";
    agents?: SkillAgent[];
  },
): void {
  assertSkillsCommandMatchesRequest(result.command, expected);
}

function assertSkillsCommandMatchesRequest(
  command: SkillsMutationCommand,
  expected: {
    operation: SkillMutationOperation;
    skillId?: string;
    source?: string;
    skillName?: string;
    scope: "project" | "global";
    agents?: SkillAgent[];
  },
): void {
  if (
    command.operation !== expected.operation ||
    command.scope !== expected.scope ||
    command.agents.length !== (expected.agents ?? []).length ||
    command.agents.some(
      (agent, index) => agent !== (expected.agents ?? [])[index],
    ) ||
    (expected.skillName != null && command.skillName !== expected.skillName)
  ) {
    throw new Error(
      "Daemon executed a Skills command for a different request.",
    );
  }
  if (expected.operation === "import") {
    if (
      command.catalogId !== expected.skillId ||
      command.source !== expected.source
    ) {
      throw new Error(
        "Daemon executed a Skills command for a different request.",
      );
    }
  }
}

export function normalizeSkillsInspectDetail(value: unknown): PackageDetail {
  const raw = record(value);
  const skillName = boundedString(raw.skill_name, 128);
  const manager = raw.manager;
  const scope = raw.scope;
  if (
    !isSkillName(skillName) ||
    typeof manager !== "string" ||
    !MANAGERS.has(manager as SkillManager) ||
    typeof scope !== "string" ||
    !SCOPES.has(scope as SkillScope)
  ) {
    throw new Error("Daemon returned an invalid Skills detail.");
  }
  const agents = normalizeAgents(raw.agents) ?? [];
  const bindings = (Array.isArray(raw.bindings) ? raw.bindings : [])
    .map((binding) => normalizeBinding(binding, manager === "zen"))
    .filter((binding): binding is SkillBinding => binding != null);
  if (bindings.some((binding) => !agents.includes(binding.agent))) {
    throw new Error("Daemon returned an inconsistent Skills detail.");
  }
  const files = (Array.isArray(raw.files) ? raw.files : [])
    .slice(0, MAX_FILES)
    .map((candidate): PackageFile | null => {
      const file = record(candidate);
      const path = boundedString(file.path, 1024);
      const size = file.size;
      const mode = boundedString(file.mode, 16);
      if (
        !path ||
        typeof size !== "number" ||
        !Number.isSafeInteger(size) ||
        size < 0 ||
        size > 64 << 20 ||
        !mode
      ) {
        return null;
      }
      return { path, size, mode };
    })
    .filter((file): file is PackageFile => file != null);
  return {
    skillName,
    description: boundedString(raw.description, 240) || undefined,
    manager: manager as SkillManager,
    owned: raw.owned === true,
    tracked: raw.tracked === true,
    enabled: raw.enabled === true,
    canonicalPath: boundedString(raw.canonical_path, 4096) || undefined,
    sourcePath: boundedString(raw.source_path, 4096) || undefined,
    source: boundedString(raw.source, 1024) || undefined,
    sourceType: boundedString(raw.source_type, 32) || undefined,
    sourceUrl: boundedString(raw.source_url, 1024) || undefined,
    ref: boundedString(raw.ref, 128) || undefined,
    contentHash: boundedString(raw.content_hash, 64) || undefined,
    installedAt: boundedString(raw.installed_at, 64) || undefined,
    updatedAt: boundedString(raw.updated_at, 64) || undefined,
    scope: scope as SkillScope,
    agents,
    bindings,
    files: files.length > 0 ? files : undefined,
    skillMd: boundedMultilineString(raw.skill_md, 70000) || undefined,
    filePath: boundedString(raw.file_path, 1024) || undefined,
    fileContent: boundedMultilineString(raw.file_content, 70000) || undefined,
    risk: (Array.isArray(raw.risk) ? raw.risk : [])
      .map(normalizeRisk)
      .filter((risk): risk is SkillRiskSignal => risk != null),
    warnings: (Array.isArray(raw.warnings) ? raw.warnings : [])
      .map((warning) => boundedString(warning, 240))
      .filter(Boolean)
      .slice(0, 12),
    capability: normalizeCapability(raw.capability),
  };
}

function normalizeRisk(value: unknown): SkillRiskSignal | null {
  const raw = record(value);
  const type = boundedString(raw.type, 40);
  const severity = raw.severity;
  if (
    !type ||
    (severity !== "info" && severity !== "warn" && severity !== "alert")
  ) {
    return null;
  }
  return {
    type,
    detail: boundedString(raw.detail, 240) || undefined,
    severity,
    file: boundedString(raw.file, 1024) || undefined,
  };
}

export function createSkillsRequestState<T>(): SkillsRequestState<T> {
  return { status: "idle", generation: 0 };
}

export function beginSkillsRequest<T>(
  current: SkillsRequestState<T>,
  generation = current.generation + 1,
  preserveData = true,
): SkillsRequestState<T> {
  if (generation <= current.generation) {
    return current;
  }
  const data = preserveData ? skillsRequestData(current) : undefined;
  return data === undefined
    ? { status: "loading", generation }
    : { status: "loading", generation, data };
}

export function skillsRequestData<T>(
  state: SkillsRequestState<T>,
): T | undefined {
  return state.data;
}

export function completeSkillsRequest<T>(
  current: SkillsRequestState<T>,
  generation: number,
  data: T,
  empty: boolean,
): SkillsRequestState<T> {
  if (current.generation !== generation || current.status !== "loading") {
    return current;
  }
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
  if (current.generation !== generation || current.status !== "loading") {
    return current;
  }
  const data = preserveData ? skillsRequestData(current) : undefined;
  const message = error.trim() || "Request failed.";
  return data === undefined
    ? { status: "error", generation, error: message }
    : { status: "error", generation, data, error: message };
}

export function buildSkillsMutationConfirmation(
  command: SkillsMutationCommand,
): {
  title: string;
  message: string;
  confirmLabel: string;
} {
  const verb = mutationVerb(command.operation);
  const scope = scopeLabel(command.scope);
  const lines = [command.summary, "", `Scope: ${scope}`];
  if (command.agents.length > 0) {
    lines.push(
      `Target${command.agents.length === 1 ? "" : "s"}: ${command.agents
        .map(skillAgentLabel)
        .join(", ")}`,
    );
  }
  if (command.operation === "import" && command.source) {
    lines.push(`Source: ${command.source}`);
    if (command.ref) {
      lines.push(`Pinned ref: ${command.ref}`);
    }
  }
  if (command.destructive) {
    lines.push(
      "",
      "This removes the following:",
      ...command.changes.map(
        (change) =>
          `• ${change.path}${change.detail ? ` (${change.detail})` : ""}`,
      ),
    );
  } else if (command.changes.length > 0) {
    lines.push(
      "",
      "Changes:",
      ...command.changes.map((change) => `• ${change.detail || change.path}`),
    );
  }
  return {
    title: `${verb} ${command.skillName || "Skills"}?`,
    message: lines.join("\n"),
    confirmLabel: verb,
  };
}

function mutationVerb(operation: SkillMutationOperation): string {
  switch (operation) {
    case "import":
      return "Import";
    case "migrate":
      return "Discover";
    case "bind":
      return "Bind";
    case "unbind":
      return "Unbind";
    case "enable":
      return "Enable";
    case "disable":
      return "Disable";
    case "uninstall":
      return "Uninstall";
    case "forget":
      return "Forget";
    case "adopt":
      return "Adopt";
    case "update":
      return "Update";
  }
}

export function skillAgentLabel(agent: SkillAgent): string {
  switch (agent) {
    case "codex":
      return "Codex";
    case "claude-code":
      return "Claude Code";
    case "cursor":
      return "Cursor";
    case "grok":
      return "Grok";
    case "opencode":
      return "OpenCode";
    case "pi":
      return "Pi";
  }
}

export function scopeLabel(scope: SkillScope | "project" | "global"): string {
  switch (scope) {
    case "project":
      return "Project";
    case "global":
      return "Global";
    case "mixed":
      return "Mixed scopes";
    case "plugin":
      return "Plugin";
    case "builtin":
      return "Builtin";
    default:
      return "Unknown";
  }
}

export function isCatalogIdentity(
  id: string,
  source: string,
  name: string,
): boolean {
  return (
    isRepository(source) && isSkillName(name) && id === `${source}/${name}`
  );
}

export function isSkillName(value: string): boolean {
  return value !== "." && value !== ".." && SKILL_PATTERN.test(value);
}

export function isRepository(value: string): boolean {
  const parts = value.split("/");
  return (
    parts.length === 2 &&
    OWNER_PATTERN.test(parts[0] || "") &&
    REPO_PATTERN.test(parts[1] || "") &&
    !parts[1]?.toLowerCase().endsWith(".git")
  );
}

// isValidMutationCommand is the plan review gate: bounded, deterministic, and
// fail-closed. The App renders exactly this plan; nothing else is actionable.
function isValidMutationCommand(command: SkillsMutationCommand): boolean {
  if (command.summary.length > 512) {
    return false;
  }
  if (command.operation !== "migrate" && command.skillName.length === 0) {
    return false;
  }
  if (command.operation === "migrate" && command.changes.length === 0) {
    return false;
  }
  return command.changes.every(
    (change) => change.path.length > 0 && change.path.length <= 4096,
  );
}

function normalizeSkillsLeaderboard(
  value: unknown,
  expectedView: SkillsLeaderboardView,
): SkillsLeaderboard {
  const raw = record(value);
  if (raw.view !== expectedView) {
    throw new Error("Daemon returned a mismatched Skills leaderboard view.");
  }
  const totalSkills = raw.total_skills;
  const rawSkills = raw.skills;
  if (
    typeof totalSkills !== "number" ||
    !Number.isSafeInteger(totalSkills) ||
    totalSkills < 0 ||
    totalSkills > MAX_LEADERBOARD_TOTAL ||
    !Array.isArray(rawSkills) ||
    rawSkills.length > MAX_LEADERBOARD_SKILLS ||
    totalSkills < rawSkills.length
  ) {
    throw new Error("Daemon returned invalid Skills leaderboard bounds.");
  }

  const byIdentity = new Map<string, RankedCatalogSkill>();
  for (let index = 0; index < rawSkills.length; index += 1) {
    let candidate: RankedCatalogSkill;
    try {
      candidate = normalizeRankedCatalogSkill(
        rawSkills[index],
        expectedView,
        0,
      );
    } catch {
      continue;
    }
    const current = byIdentity.get(candidate.id);
    if (
      !current ||
      leaderboardPrimaryMetric(candidate, expectedView) >
        leaderboardPrimaryMetric(current, expectedView) ||
      (leaderboardPrimaryMetric(candidate, expectedView) ===
        leaderboardPrimaryMetric(current, expectedView) &&
        candidate.name < current.name)
    ) {
      byIdentity.set(candidate.id, candidate);
    }
  }
  const skills = [...byIdentity.values()]
    .sort((left, right) => {
      const metricDifference =
        leaderboardPrimaryMetric(right, expectedView) -
        leaderboardPrimaryMetric(left, expectedView);
      return (
        metricDifference ||
        left.source.localeCompare(right.source) ||
        left.skillId.localeCompare(right.skillId)
      );
    })
    .map((skill, index) => ({ ...skill, rank: index + 1 }));
  return {
    view: expectedView,
    totalSkills: Math.max(totalSkills, skills.length),
    skills,
    warning: boundedString(raw.warning, 240) || undefined,
  };
}

function normalizeRankedCatalogSkill(
  value: unknown,
  view: SkillsLeaderboardView,
  expectedRank: number,
): RankedCatalogSkill {
  const raw = record(value);
  const id = boundedString(raw.id, 272);
  const skillId = boundedString(raw.skill_id, 128);
  const name = boundedString(raw.name, 128);
  const source = boundedString(raw.source, 141);
  if (
    typeof raw.installable !== "boolean" ||
    !isLeaderboardSkillID(skillId) ||
    !isLeaderboardSource(source) ||
    id !== `${source}/${skillId}` ||
    !name ||
    name.trim() !== name ||
    /[\u0000-\u001f\u007f]/.test(name) ||
    raw.installable !== isCatalogIdentity(id, source, skillId)
  ) {
    throw new Error("Daemon returned an invalid ranked Skill identity.");
  }
  const result: RankedCatalogSkill = {
    id,
    skillId,
    name,
    source,
    rank: expectedRank,
    installable: raw.installable,
  };
  if (view === "all-time") {
    result.totalInstalls = catalogMetric(raw.total_installs);
  } else if (view === "trending") {
    result.installs24h = catalogMetric(raw.installs_24h);
  } else {
    result.currentInstalls = catalogMetric(raw.current_installs);
    result.yesterdayInstalls = catalogMetric(raw.yesterday_installs);
    const change = raw.change;
    if (
      typeof change !== "number" ||
      !Number.isSafeInteger(change) ||
      change < -MAX_CATALOG_METRIC ||
      change > MAX_CATALOG_METRIC ||
      change !== result.currentInstalls - result.yesterdayInstalls
    ) {
      throw new Error("Daemon returned invalid Hot leaderboard metrics.");
    }
    result.change = change;
  }
  return result;
}

function leaderboardPrimaryMetric(
  skill: RankedCatalogSkill,
  view: SkillsLeaderboardView,
): number {
  if (view === "all-time") return skill.totalInstalls!;
  if (view === "trending") return skill.installs24h!;
  return skill.currentInstalls!;
}

function catalogMetric(value: unknown): number {
  if (
    typeof value !== "number" ||
    !Number.isSafeInteger(value) ||
    value < 0 ||
    value > MAX_CATALOG_METRIC
  ) {
    throw new Error("Daemon returned an invalid Skills leaderboard metric.");
  }
  return value;
}

function isLeaderboardSource(value: string): boolean {
  if (isRepository(value)) return true;
  if (!value || value.length > 141 || value !== value.toLowerCase())
    return false;
  const labels = value.split(".");
  return (
    labels.length >= 2 &&
    labels.every(
      (label) =>
        label.length <= 63 &&
        /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label),
    )
  );
}

function isLeaderboardSkillID(value: string): boolean {
  return (
    value !== "." &&
    value !== ".." &&
    /^[a-z0-9](?:[a-z0-9._:-]{0,126}[a-z0-9])?$/.test(value)
  );
}

function normalizeInstalledSkill(value: unknown): InstalledSkill | null {
  const raw = record(value);
  const id = boundedString(raw.id, 24);
  const name = boundedString(raw.name, 128);
  const canonicalPath = boundedString(raw.canonical_path, 4096);
  const sourcePath = boundedString(raw.source_path, 4096);
  const scope = raw.scope;
  const manager = raw.manager;
  if (
    !INSTALLED_ID_PATTERN.test(id) ||
    !name ||
    !canonicalPath ||
    !sourcePath ||
    typeof scope !== "string" ||
    !SCOPES.has(scope as SkillScope) ||
    typeof manager !== "string" ||
    !MANAGERS.has(manager as SkillManager)
  ) {
    return null;
  }
  const agents = normalizeAgents(raw.agents);
  if (agents == null) {
    return null;
  }
  const rawBindings = Array.isArray(raw.bindings) ? raw.bindings : [];
  if (rawBindings.length > MAX_BINDINGS) {
    return null;
  }
  const bindings = rawBindings.map((binding) =>
    normalizeBinding(binding, manager === "zen"),
  );
  if (bindings.some((binding) => binding == null)) {
    return null;
  }
  const validBindings = bindings as SkillBinding[];
  const bindingKeys = new Set(
    validBindings.map(
      (binding) =>
        `${binding.agent}\u0000${binding.scope}\u0000${binding.targetPath}`,
    ),
  );
  const bindingAgents = new Set(validBindings.map((binding) => binding.agent));
  if (
    bindingKeys.size !== validBindings.length ||
    agents.some((agent) => !bindingAgents.has(agent)) ||
    bindingAgents.size !== agents.length
  ) {
    return null;
  }
  const capability = normalizeCapability(raw.capability);
  return {
    id,
    name,
    description: boundedString(raw.description, 240) || undefined,
    manager: manager as SkillManager,
    owned: raw.owned === true,
    tracked: raw.tracked === true,
    enabled: raw.enabled === true,
    canonicalPath,
    sourcePath,
    scope: scope as SkillScope,
    agents,
    bindings: validBindings,
    provenance: boundedString(raw.provenance, 240) || "Unknown provenance",
    source: boundedString(raw.source, 1024) || undefined,
    sourceType: boundedString(raw.source_type, 32) || undefined,
    sourceUrl: boundedString(raw.source_url, 1024) || undefined,
    ref: boundedString(raw.ref, 128) || undefined,
    contentHash: boundedString(raw.content_hash, 64) || undefined,
    installedAt: boundedString(raw.installed_at, 64) || undefined,
    updatedAt: boundedString(raw.updated_at, 64) || undefined,
    plugin: boundedString(raw.plugin, 128) || undefined,
    risk: (Array.isArray(raw.risk) ? raw.risk : [])
      .map(normalizeRisk)
      .filter((risk): risk is SkillRiskSignal => risk != null),
    warnings: (Array.isArray(raw.warnings) ? raw.warnings : [])
      .map((warning) => boundedString(warning, 240))
      .filter(Boolean)
      .slice(0, 12),
    migration: boundedString(raw.migration, 40) || undefined,
    capability,
  };
}

function normalizeCapability(value: unknown): SkillManagementCapability {
  const raw = record(value);
  if (raw.can_manage === true) {
    const rawOps = Array.isArray(raw.operations) ? raw.operations : [];
    const operations = rawOps
      .filter(
        (op): op is SkillMutationOperation =>
          typeof op === "string" &&
          OPERATIONS.has(op as SkillMutationOperation),
      )
      .slice(0, OPERATIONS.size);
    if (operations.length !== rawOps.length || operations.length === 0) {
      return {
        canManage: false,
        operations: [],
        reason: boundedString(raw.reason, 240) || undefined,
      };
    }
    return {
      canManage: true,
      operations,
      reason: boundedString(raw.reason, 240) || undefined,
    };
  }
  return {
    canManage: false,
    operations: [],
    reason: boundedString(raw.reason, 240) || undefined,
  };
}

function normalizeBinding(
  value: unknown,
  requireBoundAt = true,
): SkillBinding | null {
  const raw = record(value);
  const agent = raw.agent;
  const scope = raw.scope;
  const mode = raw.mode;
  const targetPath = boundedString(raw.target_path, 4096);
  const sourcePath = boundedString(raw.source_path, 4096);
  const boundAt = boundedString(raw.bound_at, 64);
  const rawOperations = Array.isArray(raw.operations) ? raw.operations : [];
  const operations = rawOperations.filter(
    (operation): operation is SkillMutationOperation =>
      typeof operation === "string" &&
      (operation === "enable" ||
        operation === "disable" ||
        operation === "unbind"),
  );
  if (
    typeof agent !== "string" ||
    !AGENTS.has(agent as SkillAgent) ||
    typeof scope !== "string" ||
    !SCOPES.has(scope as SkillScope) ||
    typeof mode !== "string" ||
    !BINDING_MODES.has(mode as BindingMode) ||
    !targetPath ||
    !sourcePath ||
    (requireBoundAt && !boundAt) ||
    typeof raw.enabled !== "boolean" ||
    operations.length !== rawOperations.length
  ) {
    return null;
  }
  return {
    agent: agent as SkillAgent,
    scope: scope as SkillScope,
    mode: mode as BindingMode,
    targetPath,
    sourcePath,
    enabled: raw.enabled,
    boundAt: boundAt || undefined,
    driftHash: boundedString(raw.drift_hash, 64) || undefined,
    note: boundedString(raw.note, 240) || undefined,
    operations,
  };
}

function normalizeAgentSupport(value: unknown): SkillAgentSupport | null {
  const raw = record(value);
  const agent = raw.agent;
  const name = boundedString(raw.name, 40);
  const bindingMode = raw.binding_mode;
  const defaultGlobalDir = boundedString(raw.default_global_dir, 1024);
  if (
    typeof agent !== "string" ||
    !AGENTS.has(agent as SkillAgent) ||
    !name ||
    typeof bindingMode !== "string" ||
    !BINDING_MODES.has(bindingMode as BindingMode) ||
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
    bindingMode: bindingMode as BindingMode,
    bindingModeNote: boundedString(raw.binding_mode_note, 240) || undefined,
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
  // Go encodes a nil []Agent as null. It is valid inventory truth: canonical
  // Skills and bindings can exist without a linked supported agent target.
  if (value == null) {
    return [];
  }
  if (!Array.isArray(value) || value.length > AGENTS.size) {
    return null;
  }
  const seen = new Set<SkillAgent>();
  for (const raw of value) {
    if (typeof raw !== "string" || !AGENTS.has(raw as SkillAgent)) {
      return null;
    }
    seen.add(raw as SkillAgent);
  }
  return [...seen];
}

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function exactRecord(
  value: unknown,
  expectedKeys: readonly string[],
): Record<string, unknown> {
  const raw = record(value);
  const keys = Object.keys(raw).sort();
  const expected = [...expectedKeys].sort();
  if (
    keys.length !== expected.length ||
    keys.some((key, index) => key !== expected[index])
  ) {
    throw new Error("Daemon returned an unexpected Skills catalog shape.");
  }
  return raw;
}

function boundedString(value: unknown, maxLength: number): string {
  if (
    typeof value !== "string" ||
    value.length > maxLength ||
    /[\u0000-\u001f\u007f]/.test(value)
  ) {
    return "";
  }
  return value;
}

/** Multiline content (SKILL.md bodies) allows newlines/tabs but no other
 * control characters and no NUL. */
function boundedMultilineString(value: unknown, maxLength: number): string {
  if (
    typeof value !== "string" ||
    value.length > maxLength ||
    /[\u0000\u0001-\u0008\u000b\u000c\u000e-\u001f\u007f]/.test(value)
  ) {
    return "";
  }
  return value;
}
