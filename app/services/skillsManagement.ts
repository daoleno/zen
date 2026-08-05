export type SkillAgent = "codex" | "claude-code" | "cursor" | "opencode" | "pi" | "grok";
export type ManagedSkillAgent = Exclude<SkillAgent, "grok">;
export type SkillScope =
  "project" | "global" | "mixed" | "plugin" | "builtin" | "unknown";
export type SkillManager = "skills-cli" | "plugin" | "builtin" | "unknown";
export type SkillMutationOperation = "install" | "remove";

export interface SkillBinding {
  sourcePath: string;
  scope: SkillScope;
  agents: SkillAgent[];
}

export interface SkillAgentSupport {
  agent: SkillAgent;
  name: string;
  supported: boolean;
  cliManaged: boolean;
  reason?: string;
}

export interface SkillRemovalPlan {
  agent: ManagedSkillAgent;
  affectedAgents: ManagedSkillAgent[];
}

export type SkillManagementCapability =
  | {
      canRemove: true;
      removalPlans: SkillRemovalPlan[];
      reason?: undefined;
    }
  | {
      canRemove: false;
      removalPlans: [];
      reason?: string;
    };

export interface InstalledSkill {
  id: string;
  name: string;
  description?: string;
  canonicalPath: string;
  sourcePath: string;
  scope: SkillScope;
  agents: SkillAgent[];
  bindings: SkillBinding[];
  manager: SkillManager;
  provenance: string;
  source?: string;
  sourceType?: string;
  plugin?: string;
  capability: SkillManagementCapability;
}

export interface SkillsInventory {
  generatedAt: string;
  cwd?: string;
  skills: InstalledSkill[];
  agents: SkillAgentSupport[];
  warnings: string[];
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
}

export interface SkillsLeaderboards {
  allTime: SkillsLeaderboard;
  trending: SkillsLeaderboard;
  hot: SkillsLeaderboard;
}

export interface SkillsMutationCommand {
  operation: SkillMutationOperation;
  command: string;
  catalogId?: string;
  source?: string;
  skillName: string;
  scope: "project" | "global";
  agents: ManagedSkillAgent[];
}

export type SkillsRequestState<T> =
  | { status: "idle"; generation: number; data?: undefined; error?: undefined }
  | {
      status: "loading";
      generation: number;
      data?: undefined;
      error?: undefined;
    }
  | { status: "ready"; generation: number; data: T; error?: undefined }
  | { status: "empty"; generation: number; data: T; error?: undefined }
  | { status: "error"; generation: number; data?: undefined; error: string };

const AGENTS = new Set<SkillAgent>(["codex", "claude-code", "cursor", "opencode", "pi", "grok"]);
const MANAGED_AGENTS = new Set<ManagedSkillAgent>([
  "codex",
  "claude-code",
  "cursor",
  "opencode",
  "pi",
]);
const SCOPES = new Set<SkillScope>([
  "project",
  "global",
  "mixed",
  "plugin",
  "builtin",
  "unknown",
]);
const MANAGERS = new Set<SkillManager>([
  "skills-cli",
  "plugin",
  "builtin",
  "unknown",
]);
const OPERATIONS = new Set<SkillMutationOperation>(["install", "remove"]);
const SKILL_PATTERN = /^[a-z0-9](?:[a-z0-9._-]{0,126}[a-z0-9])?$/;
const OWNER_PATTERN = /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$/;
const REPO_PATTERN = /^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,98}[A-Za-z0-9])?$/;
const INSTALLED_ID_PATTERN = /^[a-f0-9]{24}$/;
const MAX_INVENTORY_SKILLS = 600;
const MAX_CATALOG_SKILLS = 30;
const MAX_LEADERBOARD_SKILLS = 30;
const MAX_LEADERBOARD_TOTAL = 10_000_000;
const MAX_CATALOG_METRIC = 1_000_000_000_000;

export function normalizeSkillsInventory(value: unknown): SkillsInventory {
  const inventory = record(value);
  const rawSkills = Array.isArray(inventory.skills) ? inventory.skills : [];
  if (rawSkills.length > MAX_INVENTORY_SKILLS) {
    throw new Error("Daemon returned too many installed Skills.");
  }
  const skills = rawSkills
    .map(normalizeInstalledSkill)
    .filter((skill): skill is InstalledSkill => skill != null);
  if (skills.length !== rawSkills.length) {
    throw new Error("Daemon returned an invalid installed Skill.");
  }
  const installedIDs = new Set<string>();
  const canonicalPaths = new Set<string>();
  for (const skill of skills) {
    if (installedIDs.has(skill.id) || canonicalPaths.has(skill.canonicalPath)) {
      throw new Error("Daemon returned a duplicate installed Skill.");
    }
    installedIDs.add(skill.id);
    canonicalPaths.add(skill.canonicalPath);
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
  return {
    generatedAt,
    cwd: boundedString(inventory.cwd, 4096) || undefined,
    skills,
    agents,
    warnings: (Array.isArray(inventory.warnings) ? inventory.warnings : [])
      .map((warning) => boundedString(warning, 240))
      .filter(Boolean)
      .slice(0, 12),
  };
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
      throw new Error("Daemon returned an invalid catalog identity.");
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
  const command = boundedString(raw.command, 1024);
  if (
    typeof operation !== "string" ||
    !OPERATIONS.has(operation as SkillMutationOperation) ||
    (scope !== "project" && scope !== "global") ||
    !isSkillName(skillName) ||
    !command
  ) {
    throw new Error("Daemon returned an invalid Skills command.");
  }
  const agents = normalizeManagedAgents(raw.agents);
  const normalized: SkillsMutationCommand = {
    operation: operation as SkillMutationOperation,
    command,
    skillName,
    scope,
    agents,
  };
  if (operation === "install") {
    const catalogId = boundedString(raw.catalog_id, 272);
    const source = boundedString(raw.source, 141);
    if (!isCatalogIdentity(catalogId, source, skillName)) {
      throw new Error("Daemon returned an unbound Skills install command.");
    }
    normalized.catalogId = catalogId;
    normalized.source = source;
  }
  if (!isExactOfficialSkillsCommand(normalized)) {
    throw new Error("Daemon returned a non-official Skills command.");
  }
  return normalized;
}

export function createSkillsRequestState<T>(): SkillsRequestState<T> {
  return { status: "idle", generation: 0 };
}

export function beginSkillsRequest<T>(
  current: SkillsRequestState<T>,
): SkillsRequestState<T> {
  return { status: "loading", generation: current.generation + 1 };
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
): SkillsRequestState<T> {
  if (current.generation !== generation || current.status !== "loading") {
    return current;
  }
  return {
    status: "error",
    generation,
    error: error.trim() || "Request failed.",
  };
}

export function buildSkillsMutationConfirmation(
  command: SkillsMutationCommand,
): {
  title: string;
  message: string;
  confirmLabel: string;
} {
  const verb = command.operation === "install" ? "Install" : "Remove";
  const agentLabel = command.operation === "install" ? "Target" : "Affected";
  const agentCardinality = command.agents.length === 1 ? "Agent" : "Agents";
  return {
    title: `${verb} ${command.skillName}?`,
    message: [
      `Skill: ${command.skillName}`,
      `Scope: ${scopeLabel(command.scope)}`,
      `${agentLabel} ${agentCardinality}: ${command.agents.map(skillAgentLabel).join(", ")}`,
      "",
      "Command:",
      command.command,
    ].join("\n"),
    confirmLabel: verb,
  };
}

export function skillAgentLabel(agent: SkillAgent): string {
  switch (agent) {
    case "codex":
      return "Codex";
    case "claude-code":
      return "Claude Code";
    case "cursor":
      return "Cursor";
    case "opencode":
      return "OpenCode";
    case "pi":
      return "Pi";
    case "grok":
      return "Grok";
  }
}

export function scopeLabel(scope: SkillScope): string {
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

function normalizeSkillsLeaderboard(
  value: unknown,
  expectedView: SkillsLeaderboardView,
): SkillsLeaderboard {
  const raw = exactRecord(value, ["view", "total_skills", "skills"]);
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

  const identities = new Set<string>();
  const skills: RankedCatalogSkill[] = [];
  let previousMetric = Number.POSITIVE_INFINITY;
  for (let index = 0; index < rawSkills.length; index += 1) {
    const candidate = normalizeRankedCatalogSkill(
      rawSkills[index],
      expectedView,
      index + 1,
    );
    if (identities.has(candidate.id)) {
      throw new Error("Daemon returned a duplicate ranked Skill identity.");
    }
    identities.add(candidate.id);
    const metric = leaderboardPrimaryMetric(candidate, expectedView);
    if (metric > previousMetric) {
      throw new Error("Daemon returned invalid Skills leaderboard order.");
    }
    previousMetric = metric;
    skills.push(candidate);
  }
  return { view: expectedView, totalSkills, skills };
}

function normalizeRankedCatalogSkill(
  value: unknown,
  view: SkillsLeaderboardView,
  expectedRank: number,
): RankedCatalogSkill {
  const metricKeys =
    view === "all-time"
      ? ["total_installs"]
      : view === "trending"
        ? ["installs_24h"]
        : ["current_installs", "yesterday_installs", "change"];
  const raw = exactRecord(value, [
    "id",
    "skill_id",
    "name",
    "source",
    "rank",
    "installable",
    ...metricKeys,
  ]);
  const id = boundedString(raw.id, 272);
  const skillId = boundedString(raw.skill_id, 128);
  const name = boundedString(raw.name, 128);
  const source = boundedString(raw.source, 141);
  if (
    raw.rank !== expectedRank ||
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
  if (rawBindings.length < 1 || rawBindings.length > 12) {
    return null;
  }
  const bindings = rawBindings.map(normalizeBinding);
  if (bindings.some((binding) => binding == null)) {
    return null;
  }
  const validBindings = bindings as SkillBinding[];
  const bindingPaths = new Set(
    validBindings.map((binding) => binding.sourcePath),
  );
  const bindingScopes = new Set(validBindings.map((binding) => binding.scope));
  const bindingAgents = new Set(
    validBindings.flatMap((binding) => binding.agents),
  );
  if (
    bindingPaths.size !== validBindings.length ||
    !bindingPaths.has(sourcePath) ||
    (scope === "mixed"
      ? bindingScopes.size < 2
      : validBindings.some((binding) => binding.scope !== scope)) ||
    agents.some((agent) => !bindingAgents.has(agent)) ||
    bindingAgents.size !== agents.length
  ) {
    return null;
  }
  const capability = record(raw.capability);
  const cliManaged = manager === "skills-cli" && isSkillName(name);
  const source = boundedString(raw.source, 141);
  const removable =
    cliManaged &&
    (scope === "project" || scope === "global") &&
    capability.can_remove === true;
  const removalPlans = removable
    ? normalizeRemovalPlans(capability.removal_plans, agents)
    : null;
  return {
    id,
    name,
    description: boundedString(raw.description, 240) || undefined,
    canonicalPath,
    sourcePath,
    scope: scope as SkillScope,
    agents,
    bindings: validBindings,
    manager: manager as SkillManager,
    provenance: boundedString(raw.provenance, 240) || "Unknown provenance",
    source: source && isRepository(source) ? source : undefined,
    sourceType: boundedString(raw.source_type, 32) || undefined,
    plugin: boundedString(raw.plugin, 128) || undefined,
    capability:
      removable && removalPlans
        ? { canRemove: true, removalPlans }
        : {
            canRemove: false,
            removalPlans: [],
            reason:
              boundedString(capability.reason, 240) ||
              (removable
                ? "No exact Agent removal plan was proven."
                : undefined),
          },
  };
}

function normalizeRemovalPlans(
  value: unknown,
  installedAgents: SkillAgent[],
): SkillRemovalPlan[] | null {
  const managedInstalled = installedAgents.filter(
    (agent): agent is ManagedSkillAgent => agent !== "grok",
  );
  if (
    !Array.isArray(value) ||
    value.length < 1 ||
    value.length !== managedInstalled.length
  ) {
    return null;
  }
  const installed = new Set(managedInstalled);
  const seen = new Set<ManagedSkillAgent>();
  const plans: SkillRemovalPlan[] = [];
  for (const candidate of value) {
    const raw = record(candidate);
    const agent = raw.agent;
    if (
      typeof agent !== "string" ||
      !MANAGED_AGENTS.has(agent as ManagedSkillAgent) ||
      !installed.has(agent as ManagedSkillAgent) ||
      seen.has(agent as ManagedSkillAgent)
    ) {
      return null;
    }
    let affectedAgents: ManagedSkillAgent[];
    try {
      affectedAgents = normalizeManagedAgents(raw.affected_agents);
    } catch {
      return null;
    }
    if (
      !affectedAgents.includes(agent as ManagedSkillAgent) ||
      affectedAgents.some((affected) => !installed.has(affected))
    ) {
      return null;
    }
    seen.add(agent as ManagedSkillAgent);
    plans.push({
      agent: agent as ManagedSkillAgent,
      affectedAgents,
    });
  }
  return seen.size === installed.size ? plans : null;
}

function normalizeBinding(value: unknown): SkillBinding | null {
  const raw = record(value);
  const sourcePath = boundedString(raw.source_path, 4096);
  const scope = raw.scope;
  const agents = normalizeAgents(raw.agents);
  if (
    !sourcePath ||
    typeof scope !== "string" ||
    !SCOPES.has(scope as SkillScope) ||
    agents == null
  ) {
    return null;
  }
  return { sourcePath, scope: scope as SkillScope, agents };
}

function normalizeAgentSupport(value: unknown): SkillAgentSupport | null {
  const raw = record(value);
  const agent = raw.agent;
  const name = boundedString(raw.name, 40);
  if (typeof agent !== "string" || !AGENTS.has(agent as SkillAgent) || !name) {
    return null;
  }
  return {
    agent: agent as SkillAgent,
    name,
    supported: raw.supported === true,
    cliManaged: raw.cli_managed === true,
    reason: boundedString(raw.reason, 240) || undefined,
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

function normalizeManagedAgents(value: unknown): ManagedSkillAgent[] {
  if (
    !Array.isArray(value) ||
    value.length < 1 ||
    value.length > MANAGED_AGENTS.size
  ) {
    throw new Error("Daemon returned invalid Skill targets.");
  }
  const seen = new Set<ManagedSkillAgent>();
  for (const raw of value) {
    if (
      typeof raw !== "string" ||
      !MANAGED_AGENTS.has(raw as ManagedSkillAgent)
    ) {
      throw new Error("Daemon returned an unsupported Skill target.");
    }
    if (seen.has(raw as ManagedSkillAgent)) {
      throw new Error("Daemon returned duplicate Skill targets.");
    }
    seen.add(raw as ManagedSkillAgent);
  }
  return [...seen];
}

function isExactOfficialSkillsCommand(value: SkillsMutationCommand): boolean {
  if (/[^A-Za-z0-9._:/ -]/.test(value.command)) {
    return false;
  }
  const tokens = value.command.split(" ");
  if (tokens.some((token) => token === "")) {
    return false;
  }
  let index = 0;
  if (tokens[index++] !== "npx" || tokens[index++] !== "skills") {
    return false;
  }
  const expectedSubcommand =
    value.operation === "install" ? "add" : value.operation;
  if (tokens[index++] !== expectedSubcommand) {
    return false;
  }
  if (value.operation === "install") {
    const url = tokens[index++] || "";
    const source = url.startsWith("https://github.com/")
      ? url.slice("https://github.com/".length)
      : "";
    if (
      !isRepository(source) ||
      source !== value.source ||
      value.catalogId !== `${source}/${value.skillName}` ||
      tokens[index++] !== "--skill" ||
      tokens[index++] !== value.skillName
    ) {
      return false;
    }
  } else if (tokens[index++] !== value.skillName) {
    return false;
  }

  if (value.scope === "global") {
    if (tokens[index++] !== "--global") return false;
  } else if (tokens[index] === "--global") {
    return false;
  }
  for (const agent of value.agents) {
    if (tokens[index++] !== "--agent" || tokens[index++] !== agent) {
      return false;
    }
  }
  return tokens[index++] === "--yes" && index === tokens.length;
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
