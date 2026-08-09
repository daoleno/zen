import type {
  AvailablePlugin,
  InstalledPluginRow,
  PluginInventory,
  PluginMutationOperation,
} from "./pluginsManagement";

/**
 * Plugins surface model.
 *
 * The Plugins section has two authoritative presentations: Installed rows and
 * the Explore catalog, both derived from the owning client's plugin manager
 * via the daemon `plugins_inventory` wire.
 *
 * The mutation gate is the single fail-closed authority: install requires a
 * ready catalog entry, update/uninstall require an installed Claude-hosted
 * mutable row. Codex-hosted plugins have no stable lifecycle adapter in this
 * release and are always unsupported (a visual state, never a fake button).
 */

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
export const PLUGIN_CACHE_READONLY_REASON =
  "This plugin is only visible through its cache and cannot be managed.";

/** Progressive disclosure keeps the plugin list calm while bounded. */
export const MAX_EXPANDED_PLUGINS = 24;

export interface PluginExpansionState {
  expanded: readonly string[];
}

export type PluginExpansionAction =
  | { type: "toggle"; pluginId: string }
  | { type: "reset" };

export interface PluginSectionView {
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
      catalogReady: false,
      installed: [],
      explore: [],
    };
  }
  const catalogReady = inventory.catalog.status === "ready";
  return {
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
      // The owning client's catalog is the only lifecycle authority: a cache
      // path must never authorize update/uninstall, and codex-hosted rows
      // have no stable lifecycle adapter in this release.
      if (row.source !== "catalog") {
        return {
          supported: false,
          reason:
            row.host === "codex"
              ? PLUGIN_HOST_UNSUPPORTED_REASON
              : PLUGIN_CACHE_READONLY_REASON,
        };
      }
      if (row.host !== "claude") {
        return {
          supported: false,
          reason: PLUGIN_HOST_UNSUPPORTED_REASON,
        };
      }
      if (!row.mutable) {
        return {
          supported: false,
          reason: "This plugin cannot be managed from Zen.",
        };
      }
      return {
        supported: true,
        operation: intent.kind,
      };
    }
  }
}
