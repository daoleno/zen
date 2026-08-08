/**
 * Plugins wire boundary. Plugin lifecycle is owned by the hosting client (the
 * Claude Code plugin manager in this release); the daemon relays the owning
 * client's authoritative catalog and builds exact reviewed commands. Nothing
 * here invents plugin state: catalog availability, installed rows, and
 * mutation capability all come from the daemon wire.
 */

export type PluginHost = "claude" | "codex";
export type PluginMutationOperation = "install" | "update" | "uninstall";
export type PluginCatalogStatus = "ready" | "unavailable";

export interface CatalogInstalledPlugin {
  id: string;
  version: string;
  enabled: boolean;
}

export interface AvailablePlugin {
  pluginId: string;
  name: string;
  marketplaceName: string;
  description?: string;
  sourceUrl?: string;
  sourceRef?: string;
  installable: boolean;
}

export interface PluginCatalogState {
  status: PluginCatalogStatus;
  available: AvailablePlugin[];
  installed: CatalogInstalledPlugin[];
  code?: string;
  message?: string;
}

export interface PluginHostedSkill {
  name: string;
  canonicalPath: string;
  sourcePath: string;
}

export interface InstalledPluginRow {
  id: string;
  name: string;
  marketplace: string;
  version: string;
  scope: string;
  enabled: boolean;
  host: PluginHost;
  mutable: boolean;
  source: "catalog" | "cache";
  skillCount: number;
  skills: PluginHostedSkill[];
}

export interface PluginInventory {
  generatedAt: string;
  catalog: PluginCatalogState;
  installed: InstalledPluginRow[];
  warnings: string[];
}

export interface PluginMutationCommand {
  operation: PluginMutationOperation;
  command: string;
  pluginId: string;
  scope: "user";
  host: PluginHost;
}

export const PLUGIN_MUTATION_OPERATIONS = new Set<PluginMutationOperation>([
  "install",
  "update",
  "uninstall",
]);
export const PLUGIN_HOSTS = new Set<PluginHost>(["claude", "codex"]);
const PLUGIN_ID_PATTERN = /^[a-z0-9][a-z0-9._-]{0,63}@[a-z0-9][a-z0-9-]{0,63}$/;
const PLUGIN_VERSION_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$/;
const MAX_PLUGIN_ID_LENGTH = 141;
const MAX_CATALOG_PLUGINS = 512;
const MAX_INSTALLED_PLUGINS = 128;
const MAX_PLUGIN_HOSTED_SKILLS = 128;
const MAX_PLUGIN_WARNINGS = 12;

export function normalizePluginsInventory(value: unknown): PluginInventory {
  const raw = record(value);
  const generatedAt = boundedString(raw.generated_at, 64);
  if (!generatedAt || Number.isNaN(Date.parse(generatedAt))) {
    throw new Error("Daemon returned an invalid Plugins inventory timestamp.");
  }
  const catalog = normalizeCatalogState(raw.catalog);
  const rawInstalled = Array.isArray(raw.installed) ? raw.installed : [];
  if (rawInstalled.length > MAX_INSTALLED_PLUGINS) {
    throw new Error("Daemon returned too many installed plugins.");
  }
  const installed: InstalledPluginRow[] = [];
  const ids = new Set<string>();
  for (const candidate of rawInstalled) {
    const row = normalizeInstalledPlugin(candidate);
    if (row == null || ids.has(row.id)) {
      throw new Error("Daemon returned an invalid installed plugin.");
    }
    ids.add(row.id);
    installed.push(row);
  }
  return {
    generatedAt,
    catalog,
    installed,
    warnings: (Array.isArray(raw.warnings) ? raw.warnings : [])
      .map((warning) => boundedString(warning, 240))
      .filter(Boolean)
      .slice(0, MAX_PLUGIN_WARNINGS),
  };
}

export function normalizePluginMutationCommand(
  value: unknown,
): PluginMutationCommand {
  const raw = record(value);
  const operation = raw.operation;
  const scope = raw.scope;
  const host = raw.host;
  const pluginId = boundedString(raw.plugin_id, MAX_PLUGIN_ID_LENGTH);
  const command = boundedString(raw.command, 1024);
  if (
    typeof operation !== "string" ||
    !PLUGIN_MUTATION_OPERATIONS.has(operation as PluginMutationOperation) ||
    scope !== "user" ||
    typeof host !== "string" ||
    !PLUGIN_HOSTS.has(host as PluginHost) ||
    !isPluginID(pluginId) ||
    !command
  ) {
    throw new Error("Daemon returned an invalid plugin command.");
  }
  const normalized: PluginMutationCommand = {
    operation: operation as PluginMutationOperation,
    command,
    pluginId,
    scope: "user",
    host: host as PluginHost,
  };
  if (!isExactOfficialPluginCommand(normalized)) {
    throw new Error("Daemon returned a non-official plugin command.");
  }
  return normalized;
}

export function isPluginID(value: string): boolean {
  return (
    value !== "" &&
    value.length <= MAX_PLUGIN_ID_LENGTH &&
    PLUGIN_ID_PATTERN.test(value)
  );
}

export function pluginMutationLabel(
  operation: PluginMutationOperation,
): string {
  switch (operation) {
    case "install":
      return "Install";
    case "update":
      return "Update";
    case "uninstall":
      return "Uninstall";
  }
}

export function buildPluginMutationConfirmation(
  command: PluginMutationCommand,
): {
  title: string;
  message: string;
  confirmLabel: string;
} {
  const verb = pluginMutationLabel(command.operation);
  return {
    title: `${verb} ${command.pluginId}?`,
    message: [
      `Plugin: ${command.pluginId}`,
      `Scope: ${command.scope}`,
      `Manager: ${pluginHostLabel(command.host)}`, 
      "",
      "Command:",
      command.command,
    ].join("\n"),
    confirmLabel: verb,
  };
}

function normalizeCatalogState(value: unknown): PluginCatalogState {
  const raw = record(value);
  const status = raw.status;
  if (status === "unavailable") {
    return {
      status,
      available: [],
      installed: [],
      code: boundedString(raw.code, 64) || undefined,
      message: boundedString(raw.message, 240) || undefined,
    };
  }
  if (status !== "ready") {
    throw new Error("Daemon returned an invalid plugin catalog state.");
  }
  const rawAvailable = Array.isArray(raw.available) ? raw.available : [];
  if (rawAvailable.length > MAX_CATALOG_PLUGINS) {
    throw new Error("Daemon returned too many available plugins.");
  }
  const available: AvailablePlugin[] = [];
  const seen = new Set<string>();
  for (const candidate of rawAvailable) {
    const plugin = normalizeAvailablePlugin(candidate);
    if (plugin == null || seen.has(plugin.pluginId)) {
      throw new Error("Daemon returned an invalid available plugin.");
    }
    seen.add(plugin.pluginId);
    available.push(plugin);
  }
  const rawInstalled = Array.isArray(raw.installed) ? raw.installed : [];
  if (rawInstalled.length > MAX_INSTALLED_PLUGINS) {
    throw new Error("Daemon returned too many catalog installed plugins.");
  }
  const installed: CatalogInstalledPlugin[] = [];
  const installedIds = new Set<string>();
  for (const candidate of rawInstalled) {
    const rawEntry = record(candidate);
    const id = boundedString(rawEntry.id, MAX_PLUGIN_ID_LENGTH);
    if (
      !isPluginID(id) ||
      installedIds.has(id) ||
      typeof rawEntry.enabled !== "boolean"
    ) {
      throw new Error("Daemon returned an invalid catalog installed plugin.");
    }
    installedIds.add(id);
    installed.push({
      id,
      version: boundedString(rawEntry.version, 64),
      enabled: rawEntry.enabled,
    });
  }
  return { status: "ready", available, installed };
}

function normalizeAvailablePlugin(value: unknown): AvailablePlugin | null {
  const raw = record(value);
  const pluginId = boundedString(raw.plugin_id, MAX_PLUGIN_ID_LENGTH);
  const name = boundedString(raw.name, 128);
  const marketplaceName = boundedString(raw.marketplace_name, 128);
  if (
    !isPluginID(pluginId) ||
    !name ||
    !marketplaceName ||
    typeof raw.installable !== "boolean"
  ) {
    return null;
  }
  const plugin: AvailablePlugin = {
    pluginId,
    name,
    marketplaceName,
    description: boundedString(raw.description, 400) || undefined,
    sourceUrl: boundedString(raw.source_url, 1024) || undefined,
    sourceRef: boundedString(raw.source_ref, 128) || undefined,
    installable: raw.installable,
  };
  if (!installableIdentityConsistent(plugin)) {
    return null;
  }
  return plugin;
}

function installableIdentityConsistent(plugin: AvailablePlugin): boolean {
  const [name, marketplace] = plugin.pluginId.split("@");
  return name === plugin.name && marketplace === plugin.marketplaceName;
}

function normalizeInstalledPlugin(value: unknown): InstalledPluginRow | null {
  const raw = record(value);
  const id = boundedString(raw.id, MAX_PLUGIN_ID_LENGTH);
  const name = boundedString(raw.name, 128);
  const marketplace = boundedString(raw.marketplace, 128);
  const version = boundedString(raw.version, 64);
  const scope = raw.scope;
  const host = raw.host;
  if (
    !isPluginID(id) ||
    !name ||
    !marketplace ||
    !version ||
    scope !== "user" ||
    typeof host !== "string" ||
    !PLUGIN_HOSTS.has(host as PluginHost) ||
    typeof raw.mutable !== "boolean" ||
    (raw.source !== "catalog" && raw.source !== "cache") ||
    typeof raw.enabled !== "boolean"
  ) {
    return null;
  }
  const [expectedName, expectedMarketplace] = id.split("@");
  if (name !== expectedName || marketplace !== expectedMarketplace) {
    return null;
  }
  const rawSkills = Array.isArray(raw.skills) ? raw.skills : [];
  if (rawSkills.length > MAX_PLUGIN_HOSTED_SKILLS) {
    return null;
  }
  const skills: PluginHostedSkill[] = [];
  const paths = new Set<string>();
  for (const candidate of rawSkills) {
    const rawSkill = record(candidate);
    const skillName = boundedString(rawSkill.name, 128);
    const canonicalPath = boundedString(rawSkill.canonical_path, 4096);
    const sourcePath = boundedString(rawSkill.source_path, 4096);
    if (
      !skillName ||
      !canonicalPath ||
      !sourcePath ||
      paths.has(sourcePath)
    ) {
      return null;
    }
    paths.add(sourcePath);
    skills.push({ name: skillName, canonicalPath, sourcePath });
  }
  if (skills.length !== rawSkills.length) {
    return null;
  }
  const skillCount = raw.skill_count;
  if (
    typeof skillCount !== "number" ||
    !Number.isSafeInteger(skillCount) ||
    skillCount < 0 ||
    skillCount > MAX_PLUGIN_HOSTED_SKILLS ||
    skillCount !== skills.length
  ) {
    return null;
  }
  return {
    id,
    name,
    marketplace,
    version,
    scope: "user",
    enabled: raw.enabled,
    host: host as PluginHost,
    mutable: raw.mutable,
    source: raw.source as "catalog" | "cache",
    skillCount,
    skills,
  };
}

/**
 * Exact official plugin-manager commands only. Every token is a validated
 * literal; no shell syntax can pass.
 */
function isExactOfficialPluginCommand(
  value: PluginMutationCommand,
): boolean {
  if (/[^A-Za-z0-9._@:/ -]/.test(value.command)) {
    return false;
  }
  const tokens = value.command.split(" ");
  if (tokens.some((token) => token === "")) {
    return false;
  }
  let index = 0;
  if (tokens[index++] !== "claude" || tokens[index++] !== "plugin") {
    return false;
  }
  const verb = tokens[index++] || "";
  const expectedVerb =
    value.operation === "install"
      ? "install"
      : value.operation === "update"
        ? "update"
        : "uninstall";
  if (verb !== expectedVerb) {
    return false;
  }
  if (tokens[index++] !== value.pluginId) {
    return false;
  }
  if (tokens[index++] !== "--scope" || tokens[index++] !== "user") {
    return false;
  }
  if (value.operation === "uninstall") {
    if (tokens[index++] !== "--yes") {
      return false;
    }
  }
  return index === tokens.length;
}

export function pluginHostLabel(host: PluginHost): string {
  return host === "claude" ? "Claude Code" : "Codex";
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
