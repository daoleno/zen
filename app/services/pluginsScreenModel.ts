import type { ManagedSkillAgent } from "./skillsManagement";
import { skillAgentLabel } from "./skillsManagement";
import type { AvailablePlugin, InstalledPluginCopy } from "./pluginsManagement";
import { pluginAgentLabel, pluginHostLabel } from "./pluginsManagement";

export type PluginCapabilityFilter = "all" | "uninstallable" | "readonly";

export interface PluginFilters {
  agents: ManagedSkillAgent[];
  capability: PluginCapabilityFilter;
}

export interface LogicalPlugin {
  key: string;
  name: string;
  displayName: string;
  description?: string;
  copies: InstalledPluginCopy[];
  agents: string[];
  versions: string[];
  canUninstall: boolean;
}

export function groupLogicalPlugins(
  copies: InstalledPluginCopy[],
): LogicalPlugin[] {
  const groups = new Map<string, InstalledPluginCopy[]>();
  for (const copy of copies) {
    const key = copy.name.toLocaleLowerCase();
    const group = groups.get(key) ?? [];
    group.push(copy);
    groups.set(key, group);
  }
  return [...groups.entries()]
    .map(([key, values]) => {
      const sorted = [...values].sort(comparePluginCopies);
      const agents = unique(sorted.flatMap((copy) => copy.agents)).sort(
        (left, right) => left.localeCompare(right),
      );
      const versions = unique(
        sorted
          .map((copy) => copy.version)
          .filter((version): version is string =>
            Boolean(version && version !== "unknown"),
          ),
      ).sort((left, right) => left.localeCompare(right));
      const display = sorted.find((copy) => copy.displayName)?.displayName;
      return {
        key,
        name: sorted[0]!.name,
        displayName: display || sorted[0]!.name,
        description: sorted.find((copy) => copy.description)?.description,
        copies: sorted,
        agents,
        versions,
        canUninstall: sorted.some((copy) => copy.capability.canUninstall),
      };
    })
    .sort((left, right) => left.displayName.localeCompare(right.displayName));
}

export function filterLogicalPlugins(
  plugins: LogicalPlugin[],
  query: string,
  filters: PluginFilters,
): LogicalPlugin[] {
  const normalized = query.trim().toLocaleLowerCase();
  return plugins.filter((plugin) => {
    if (
      filters.agents.length > 0 &&
      !filters.agents.some((agent) => plugin.agents.includes(agent))
    ) {
      return false;
    }
    if (filters.capability === "uninstallable" && !plugin.canUninstall) {
      return false;
    }
    if (filters.capability === "readonly" && plugin.canUninstall) {
      return false;
    }
    if (!normalized) return true;
    return pluginSearchValues(plugin).some((value) =>
      value.toLocaleLowerCase().includes(normalized),
    );
  });
}

export function evaluatePluginUninstall(
  copy: InstalledPluginCopy,
): { supported: true } | { supported: false; reason: string } {
  if (copy.capability.canUninstall) return { supported: true };
  return {
    supported: false,
    reason:
      copy.capability.reason ||
      "This Plugin copy cannot be uninstalled from here.",
  };
}

export function evaluatePluginInstall(
  entry: AvailablePlugin,
): { supported: true } | { supported: false; reason: string } {
  if (entry.installable) return { supported: true };
  return { supported: false, reason: "This Plugin is already installed." };
}

export function pluginRowMetadata(plugin: LogicalPlugin): string {
  if (plugin.copies.length > 1) {
    if (plugin.versions.length <= 2) {
      return plugin.versions.map((version) => `v${version}`).join(" · ");
    }
    return `${plugin.versions.length} versions`;
  }
  const copy = plugin.copies[0]!;
  return [
    copy.version && copy.version !== "unknown" ? `v${copy.version}` : "",
    `@${copy.marketplace}`,
  ]
    .filter(Boolean)
    .join(" · ");
}

export function pluginCopyLabel(copy: InstalledPluginCopy): string {
  return [
    pluginHostLabel(copy.host),
    copy.version && copy.version !== "unknown" ? `v${copy.version}` : "",
    `@${copy.marketplace}`,
  ]
    .filter(Boolean)
    .join(" · ");
}

export function pluginReadonlyReason(copy: InstalledPluginCopy): string {
  return (
    copy.capability.reason ||
    `Provided by ${pluginHostLabel(copy.host)} and cannot be removed here.`
  );
}

function pluginSearchValues(plugin: LogicalPlugin): string[] {
  return [
    plugin.name,
    plugin.displayName,
    plugin.description || "",
    ...plugin.versions,
    ...plugin.agents.map(pluginAgentLabel),
    ...plugin.copies.flatMap((copy) => [
      copy.pluginId,
      copy.marketplace,
      copy.location,
      pluginHostLabel(copy.host),
      ...copy.components.flatMap((component) => [
        component.name,
        component.kind,
        component.path || "",
      ]),
    ]),
  ];
}

function comparePluginCopies(
  left: InstalledPluginCopy,
  right: InstalledPluginCopy,
): number {
  return (
    left.host.localeCompare(right.host) ||
    left.marketplace.localeCompare(right.marketplace) ||
    (left.version || "").localeCompare(right.version || "") ||
    left.rootPath.localeCompare(right.rootPath)
  );
}

function unique<T>(values: T[]): T[] {
  return [...new Set(values)];
}
