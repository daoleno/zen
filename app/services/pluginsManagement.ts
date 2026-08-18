import type { ManagedSkillAgent } from "./skillsManagement";
import { skillAgentLabel } from "./skillsManagement";

export type PluginHost = "claude" | "codex";
export type PluginSource = "manager" | "cache" | "remote_cache";
export type PluginMutationOperation = "install" | "uninstall";

export interface AvailablePlugin {
  pluginId: string;
  name: string;
  displayName?: string;
  marketplaceName: string;
  version?: string;
  description?: string;
  sourceUrl?: string;
  sourceRef?: string;
  host: PluginHost;
  installable: boolean;
}

export interface PluginComponent {
  kind: string;
  name: string;
  path?: string;
}

export interface PluginCapability {
  canUninstall: boolean;
  reason?: string;
}

export interface InstalledPluginCopy {
  copyId: string;
  pluginId: string;
  name: string;
  displayName?: string;
  description?: string;
  marketplace: string;
  version?: string;
  scope: "user";
  enabled: boolean;
  host: PluginHost;
  source: PluginSource;
  rootPath: string;
  canonicalPath: string;
  allowedRoot: string;
  location: string;
  revision: string;
  agents: string[];
  components: PluginComponent[];
  capability: PluginCapability;
}

export interface PluginInventory {
  generatedAt: string;
  installed: InstalledPluginCopy[];
  available: AvailablePlugin[];
  warnings: string[];
}

export interface PluginInstallInput {
  operation: "install";
  pluginId: string;
  host: PluginHost;
  scope: "user";
}

export interface PluginUninstallInput {
  operation: "uninstall";
  pluginId: string;
  host: PluginHost;
  source: Exclude<PluginSource, "remote_cache">;
  scope: "user";
  copyId: string;
  name: string;
  version: string;
  rootPath: string;
  canonicalPath: string;
  allowedRoot: string;
  revision: string;
  agents: string[];
}

export type PluginMutationInput = PluginInstallInput | PluginUninstallInput;

export interface PluginMutationCommand {
  operation: PluginMutationOperation;
  pluginId: string;
  host: PluginHost;
  source?: PluginSource;
  scope: "user";
  copyId?: string;
  name: string;
  displayName?: string;
  version?: string;
  rootPath?: string;
  canonicalPath?: string;
  allowedRoot?: string;
  location?: string;
  revision?: string;
  agents: string[];
  summary: string;
  destructive: boolean;
}

export interface PluginMutationExecution {
  success: boolean;
  exitCode: number;
  output: string;
  durationMs: number;
}

export interface PluginMutationResult {
  command: PluginMutationCommand;
  execution: PluginMutationExecution;
}

const PLUGIN_ID_PATTERN = /^[a-z0-9][a-z0-9._-]{0,63}@[a-z0-9][a-z0-9-]{0,63}$/;
const COPY_ID_PATTERN = /^[a-f0-9]{24}$/;
const REVISION_PATTERN = /^[a-f0-9]{64}$/;
const PLUGIN_VERSION_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$/;
const PLUGIN_HOSTS = new Set<PluginHost>(["claude", "codex"]);
const PLUGIN_SOURCES = new Set<PluginSource>([
  "manager",
  "cache",
  "remote_cache",
]);
const PLUGIN_OPERATIONS = new Set<PluginMutationOperation>([
  "install",
  "uninstall",
]);
const KNOWN_PLUGIN_AGENTS = new Set<ManagedSkillAgent>([
  "codex",
  "claude-code",
  "cursor",
  "grok",
  "opencode",
  "pi",
]);
const PLUGIN_AGENT_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
const MAX_INSTALLED = 128;
const MAX_AVAILABLE = 512;
const MAX_COMPONENTS = 128;
const MAX_WARNINGS = 12;

export function normalizePluginsInventory(value: unknown): PluginInventory {
  const raw = record(value);
  const generatedAt = boundedString(raw.generated_at, 64);
  if (!generatedAt || Number.isNaN(Date.parse(generatedAt))) {
    throw new Error("Daemon returned an invalid Plugins inventory timestamp.");
  }
  const rawInstalled = Array.isArray(raw.installed) ? raw.installed : [];
  if (rawInstalled.length > MAX_INSTALLED) {
    throw new Error("Daemon returned too many installed Plugin copies.");
  }
  const installed: InstalledPluginCopy[] = [];
  const copyIds = new Set<string>();
  for (const candidate of rawInstalled) {
    const copy = normalizeInstalledPluginCopy(candidate);
    if (!copy || copyIds.has(copy.copyId)) {
      throw new Error("Daemon returned an invalid installed Plugin copy.");
    }
    copyIds.add(copy.copyId);
    installed.push(copy);
  }
  const rawAvailable = Array.isArray(raw.available) ? raw.available : [];
  if (rawAvailable.length > MAX_AVAILABLE) {
    throw new Error("Daemon returned too many available Plugins.");
  }
  const available: AvailablePlugin[] = [];
  const availableIds = new Set<string>();
  for (const candidate of rawAvailable) {
    const plugin = normalizeAvailablePlugin(candidate);
    const key = plugin ? `${plugin.host}:${plugin.pluginId}` : "";
    if (!plugin || availableIds.has(key)) {
      throw new Error("Daemon returned an invalid available Plugin.");
    }
    availableIds.add(key);
    available.push(plugin);
  }
  return {
    generatedAt,
    installed,
    available,
    warnings: (Array.isArray(raw.warnings) ? raw.warnings : [])
      .map((warning) => boundedString(warning, 240))
      .filter(Boolean)
      .slice(0, MAX_WARNINGS),
  };
}

export function normalizePluginMutationCommand(
  value: unknown,
): PluginMutationCommand {
  const raw = record(value);
  const operation = raw.operation;
  const pluginId = boundedString(raw.plugin_id, 141);
  const host = raw.host;
  const scope = raw.scope;
  const source = raw.source;
  const name = boundedString(raw.name, 128);
  const summary = boundedString(raw.summary, 400);
  const destructive = raw.destructive;
  if (
    typeof operation !== "string" ||
    !PLUGIN_OPERATIONS.has(operation as PluginMutationOperation) ||
    !isPluginID(pluginId) ||
    typeof host !== "string" ||
    !PLUGIN_HOSTS.has(host as PluginHost) ||
    scope !== "user" ||
    !name ||
    !summary ||
    typeof destructive !== "boolean"
  ) {
    throw new Error("Daemon returned an invalid Plugin command.");
  }
  const agents = normalizeAgents(raw.agents);
  const command: PluginMutationCommand = {
    operation: operation as PluginMutationOperation,
    pluginId,
    host: host as PluginHost,
    scope: "user",
    name,
    displayName: boundedString(raw.display_name, 128) || undefined,
    agents,
    summary,
    destructive,
  };
  if (operation === "install") {
    if (destructive || boundedString(raw.copy_id, 24)) {
      throw new Error("Daemon returned an invalid Plugin install command.");
    }
    return command;
  }
  const copyId = boundedString(raw.copy_id, 24);
  const version = boundedString(raw.version, 64);
  const rootPath = boundedString(raw.root_path, 4096);
  const canonicalPath = boundedString(raw.canonical_path, 4096);
  const allowedRoot = boundedString(raw.allowed_root, 4096);
  const location = boundedString(raw.location, 240);
  const revision = boundedString(raw.revision, 64);
  if (
    !COPY_ID_PATTERN.test(copyId) ||
    (source !== "manager" && source !== "cache") ||
    !PLUGIN_VERSION_PATTERN.test(version) ||
    !rootPath ||
    !canonicalPath ||
    !allowedRoot ||
    !location ||
    !REVISION_PATTERN.test(revision) ||
    !destructive
  ) {
    throw new Error("Daemon returned an incomplete Plugin uninstall command.");
  }
  return {
    ...command,
    copyId,
    source: source as Exclude<PluginSource, "remote_cache">,
    version,
    rootPath,
    canonicalPath,
    allowedRoot,
    location,
    revision,
  };
}

export function normalizePluginMutationResult(
  value: unknown,
): PluginMutationResult {
  const raw = record(value);
  const command = normalizePluginMutationCommand(raw.command);
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
    durationMs > 3_600_000 ||
    success !== (exitCode === 0)
  ) {
    throw new Error("Daemon returned an invalid Plugin mutation outcome.");
  }
  return {
    command,
    execution: {
      success,
      exitCode,
      output: boundedString(raw.output, 60_000),
      durationMs,
    },
  };
}

export function assertPluginMutationMatchesRequest(
  result: PluginMutationResult,
  expected: PluginMutationInput,
): void {
  assertPluginCommandMatchesRequest(result.command, expected);
}

export function assertPluginCommandMatchesRequest(
  command: PluginMutationCommand,
  expected: PluginMutationInput,
): void {
  if (
    command.operation !== expected.operation ||
    command.pluginId !== expected.pluginId ||
    command.host !== expected.host ||
    command.scope !== expected.scope
  ) {
    throw new Error(
      "Daemon executed a Plugin command for a different request.",
    );
  }
  if (
    expected.operation === "uninstall" &&
    (command.copyId !== expected.copyId ||
      command.name !== expected.name ||
      command.source !== expected.source ||
      command.version !== expected.version ||
      command.rootPath !== expected.rootPath ||
      command.canonicalPath !== expected.canonicalPath ||
      command.allowedRoot !== expected.allowedRoot ||
      command.revision !== expected.revision ||
      command.agents.length !== expected.agents.length ||
      command.agents.some((agent, index) => agent !== expected.agents[index]))
  ) {
    throw new Error("Daemon executed a different Plugin copy.");
  }
}

export function pluginUninstallInput(
  copy: InstalledPluginCopy,
): PluginUninstallInput {
  if (copy.source === "remote_cache") {
    throw new Error("This provider-managed Plugin cannot be removed here.");
  }
  return {
    operation: "uninstall",
    pluginId: copy.pluginId,
    host: copy.host,
    source: copy.source,
    scope: "user",
    copyId: copy.copyId,
    name: copy.name,
    version: copy.version || "unknown",
    rootPath: copy.rootPath,
    canonicalPath: copy.canonicalPath,
    allowedRoot: copy.allowedRoot,
    revision: copy.revision,
    agents: [...copy.agents],
  };
}

export function buildPluginMutationConfirmation(
  command: PluginMutationCommand,
): { title: string; message: string; confirmLabel: string } {
  const name = command.displayName || command.name;
  if (command.operation === "install") {
    return {
      title: `Install ${name}?`,
      message: [
        `Plugin: ${name}`,
        `Available to: ${command.agents.map(pluginAgentLabel).join(", ")}`,
        `Manager: ${pluginHostLabel(command.host)}`,
      ].join("\n"),
      confirmLabel: "Install",
    };
  }
  return {
    title: `Uninstall ${name}?`,
    message: [
      `Plugin: ${name}`,
      `Available to: ${command.agents.map(pluginAgentLabel).join(", ")}`,
      `Location: ${command.location}`,
      "",
      "This permanently removes this exact Plugin copy and cannot be undone.",
    ].join("\n"),
    confirmLabel: "Uninstall",
  };
}

export function pluginHostLabel(host: PluginHost): string {
  return host === "claude" ? "Claude Code" : "Codex";
}

export function pluginAgentLabel(agent: string): string {
  return KNOWN_PLUGIN_AGENTS.has(agent as ManagedSkillAgent)
    ? skillAgentLabel(agent as ManagedSkillAgent)
    : agent;
}

export function isPluginID(value: string): boolean {
  return value.length <= 141 && PLUGIN_ID_PATTERN.test(value);
}

function normalizeInstalledPluginCopy(
  value: unknown,
): InstalledPluginCopy | null {
  const raw = record(value);
  const copyId = boundedString(raw.copy_id, 24);
  const pluginId = boundedString(raw.plugin_id, 141);
  const name = boundedString(raw.name, 128);
  const marketplace = boundedString(raw.marketplace, 128);
  const host = raw.host;
  const source = raw.source;
  const rootPath = boundedString(raw.root_path, 4096);
  const canonicalPath = boundedString(raw.canonical_path, 4096);
  const allowedRoot = boundedString(raw.allowed_root, 4096);
  const location = boundedString(raw.location, 240);
  const revision = boundedString(raw.revision, 64);
  if (
    !COPY_ID_PATTERN.test(copyId) ||
    !isPluginID(pluginId) ||
    !name ||
    !marketplace ||
    pluginId !== `${name}@${marketplace}` ||
    raw.scope !== "user" ||
    typeof raw.enabled !== "boolean" ||
    typeof host !== "string" ||
    !PLUGIN_HOSTS.has(host as PluginHost) ||
    typeof source !== "string" ||
    !PLUGIN_SOURCES.has(source as PluginSource) ||
    !rootPath ||
    !canonicalPath ||
    !allowedRoot ||
    !location ||
    !REVISION_PATTERN.test(revision)
  ) {
    return null;
  }
  const rawComponents = Array.isArray(raw.components) ? raw.components : [];
  if (rawComponents.length > MAX_COMPONENTS) return null;
  const components: PluginComponent[] = [];
  const componentKeys = new Set<string>();
  for (const candidate of rawComponents) {
    const component = record(candidate);
    const kind = boundedString(component.kind, 64);
    const componentName = boundedString(component.name, 128);
    const path = boundedString(component.path, 1024) || undefined;
    const key = `${kind}\u0000${componentName}\u0000${path || ""}`;
    if (!kind || !componentName || componentKeys.has(key)) return null;
    componentKeys.add(key);
    components.push({ kind, name: componentName, path });
  }
  const capability = record(raw.capability);
  if (typeof capability.can_uninstall !== "boolean") return null;
  return {
    copyId,
    pluginId,
    name,
    displayName: boundedString(raw.display_name, 128) || undefined,
    description: boundedString(raw.description, 400) || undefined,
    marketplace,
    version: boundedString(raw.version, 64) || undefined,
    scope: "user",
    enabled: raw.enabled,
    host: host as PluginHost,
    source: source as PluginSource,
    rootPath,
    canonicalPath,
    allowedRoot,
    location,
    revision,
    agents: normalizeAgents(raw.agents),
    components,
    capability: {
      canUninstall: capability.can_uninstall,
      reason: boundedString(capability.reason, 400) || undefined,
    },
  };
}

function normalizeAvailablePlugin(value: unknown): AvailablePlugin | null {
  const raw = record(value);
  const pluginId = boundedString(raw.plugin_id, 141);
  const name = boundedString(raw.name, 128);
  const marketplaceName = boundedString(raw.marketplace_name, 128);
  const host = raw.host;
  if (
    !isPluginID(pluginId) ||
    !name ||
    !marketplaceName ||
    pluginId !== `${name}@${marketplaceName}` ||
    typeof host !== "string" ||
    !PLUGIN_HOSTS.has(host as PluginHost) ||
    typeof raw.installable !== "boolean"
  ) {
    return null;
  }
  return {
    pluginId,
    name,
    displayName: boundedString(raw.display_name, 128) || undefined,
    marketplaceName,
    version: boundedString(raw.version, 64) || undefined,
    description: boundedString(raw.description, 400) || undefined,
    sourceUrl: boundedString(raw.source_url, 1024) || undefined,
    sourceRef: boundedString(raw.source_ref, 1024) || undefined,
    host: host as PluginHost,
    installable: raw.installable,
  };
}

function normalizeAgents(value: unknown): string[] {
  if (!Array.isArray(value) || value.length > 6) {
    throw new Error("Daemon returned invalid Plugin Agent availability.");
  }
  const agents: string[] = [];
  for (const agent of value) {
    const normalized = boundedString(agent, 64);
    if (!PLUGIN_AGENT_PATTERN.test(normalized) || agents.includes(normalized)) {
      throw new Error("Daemon returned invalid Plugin Agent availability.");
    }
    agents.push(normalized);
  }
  return agents;
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
  ) {
    return "";
  }
  return value;
}
