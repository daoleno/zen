import type { SkillScope, SkillsInventory } from "./skillsManagement";
import type {
  AvailablePlugin,
  InstalledPluginRow,
  PluginInventory,
  PluginMutationOperation,
} from "./pluginsManagement";
import { pluginHostLabel } from "./pluginsManagement";

/**
 * Plugins surface model.
 *
 * The Plugins section has two authoritative presentations: Installed rows and
 * the Explore catalog, both derived from the owning client's plugin manager
 * via the daemon `plugins_inventory` wire. On daemons that predate that wire,
 * a cache-derived read-only projection from `skills_inventory` is the only
 * fallback: it shows installed plugins truthfully with every lifecycle action
 * unavailable.
 *
 * The mutation gate is the single fail-closed authority: install requires a
 * ready catalog entry, update/uninstall require an installed Claude-hosted
 * mutable row. Codex-hosted plugins have no stable lifecycle adapter in this
 * release and are always unsupported (a visual state, never a fake button).
 */

export interface CacheFallbackPlugin {
  id: string;
  name: string;
  hosts: string[];
  skillCount: number;
  skills: Array<{ name: string; scope: SkillScope; canonicalPath: string }>;
}

export type PluginMutationIntent =
  | { kind: "install"; entry: AvailablePlugin; installedIds: Set<string> }
  | { kind: "update"; row: InstalledPluginRow }
  | { kind: "uninstall"; row: InstalledPluginRow };

export type PluginMutationDecision =
  | { supported: true; operation: PluginMutationOperation }
  | { supported: false; reason: string };

export const PLUGIN_INSTALL_UNSUPPORTED_REASON =
  "The plugin catalog is unavailable on this server.";
export const PLUGIN_HOST_UNSUPPORTED_REASON =
  "This plugin's hosting client is not manageable from Zen.";

/** Progressive disclosure keeps the plugin list calm while bounded. */
export const MAX_EXPANDED_PLUGINS = 24;

export interface PluginExpansionState {
  expanded: readonly string[];
}

export type PluginExpansionAction =
  | { type: "toggle"; pluginId: string }
  | { type: "reset" };

export type PluginSourceMode = "authoritative" | "cache-fallback";

export interface PluginSectionView {
  source: PluginSourceMode;
  catalogReady: boolean;
  catalogUnavailableCode?: string;
  installed: InstalledPluginRow[];
  explore: AvailablePlugin[];
}

/**
 * The Installed/Explore view from the authoritative plugin inventory. A
 * non-ready catalog keeps installed rows but renders no install affordance
 * and no Explore list (capability gap is a state, not a guess).
 */
export function pluginSectionView(
  inventory: PluginInventory | undefined,
): PluginSectionView {
  if (!inventory) {
    return {
      source: "authoritative",
      catalogReady: false,
      installed: [],
      explore: [],
    };
  }
  const catalogReady = inventory.catalog.status === "ready";
  return {
    source: "authoritative",
    catalogReady,
    catalogUnavailableCode: catalogReady
      ? undefined
      : inventory.catalog.code,
    installed: [...inventory.installed].sort((left, right) =>
      left.id.localeCompare(right.id),
    ),
    explore: catalogReady
      ? [...inventory.catalog.available].sort((left, right) =>
          left.name.localeCompare(right.name),
        )
      : [],
  };
}

/**
 * Read-only fallback for daemons without the plugin inventory wire: projects
 * plugin-owned Skills from the authoritative Skills inventory into plugin
 * rows. Lifecycle actions are never rendered for this source.
 */
export function projectPlugins(
  inventory: SkillsInventory | undefined,
): CacheFallbackPlugin[] {
  const groups = new Map<string, CacheFallbackPlugin>();
  for (const skill of inventory?.skills ?? []) {
    if (skill.manager !== "plugin") {
      continue;
    }
    const name = pluginName(skill.plugin, skill.provenance);
    const key = name.toLocaleLowerCase();
    let plugin = groups.get(key);
    if (!plugin) {
      plugin = {
        id: `plugin:${key}`,
        name,
        hosts: [],
        skillCount: 0,
        skills: [],
      };
      groups.set(key, plugin);
    }
    plugin.skills.push({
      name: skill.name,
      scope: skill.scope,
      canonicalPath: skill.canonicalPath,
    });
    for (const agent of skill.agents) {
      if (!plugin.hosts.includes(agent)) {
        plugin.hosts.push(agent);
      }
    }
  }
  const plugins = [...groups.values()];
  for (const plugin of plugins) {
    plugin.skills.sort((left, right) => left.name.localeCompare(right.name));
    plugin.hosts.sort();
    plugin.skillCount = plugin.skills.length;
  }
  return plugins.sort((left, right) => left.name.localeCompare(right.name));
}

export function cacheFallbackHostLabel(host: string): string {
  return host === "claude-code" ? "Claude Code" : pluginHostLabel(host as never) || host;
}

export function createPluginExpansionState(): PluginExpansionState {
  return { expanded: [] };
}

export function reducePluginExpansion(
  current: PluginExpansionState,
  action: PluginExpansionAction,
): PluginExpansionState {
  switch (action.type) {
    case "toggle": {
      const { pluginId } = action;
      if (!pluginId) {
        return current;
      }
      if (current.expanded.includes(pluginId)) {
        return {
          expanded: current.expanded.filter((id) => id !== pluginId),
        };
      }
      if (current.expanded.length >= MAX_EXPANDED_PLUGINS) {
        return { expanded: [...current.expanded.slice(1), pluginId] };
      }
      return { expanded: [...current.expanded, pluginId] };
    }
    case "reset":
      return createPluginExpansionState();
  }
}

/**
 * The authoritative plugin mutation gate. Every supported decision requires
 * daemon-proven capability: a ready catalog for install, a mutable
 * Claude-hosted installed row for update/uninstall. Everything else fails
 * closed with a deterministic reason so the UI can render an honest state.
 */
export function evaluatePluginMutation(
  intent: PluginMutationIntent,
): PluginMutationDecision {
  switch (intent.kind) {
    case "install": {
      if (!intent.entry.installable) {
        return {
          supported: false,
          reason: "This plugin is already installed on this server.",
        };
      }
      if (intent.installedIds.has(intent.entry.pluginId)) {
        return {
          supported: false,
          reason: "This plugin is already installed on this server.",
        };
      }
      return { supported: true, operation: "install" };
    }
    case "update":
    case "uninstall": {
      const row = intent.row;
      if (!row.mutable) {
        return {
          supported: false,
          reason:
            row.host === "codex"
              ? PLUGIN_HOST_UNSUPPORTED_REASON
              : "This plugin cannot be managed from Zen.",
        };
      }
      if (row.host !== "claude") {
        return {
          supported: false,
          reason: PLUGIN_HOST_UNSUPPORTED_REASON,
        };
      }
      return {
        supported: true,
        operation: intent.kind,
      };
    }
  }
}

function pluginName(plugin: string | undefined, provenance: string): string {
  const direct = plugin?.trim();
  if (direct) {
    return direct;
  }
  return provenance.trim() || "Plugin";
}
