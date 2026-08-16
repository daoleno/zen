import type {
  AvailablePlugin,
  InstalledPluginRow,
  PluginInventory,
  PluginMutationOperation,
} from "./pluginsManagement";

/**
 * Plugins surface model.
 *
 * The Plugins section is one stable management list: installed rows and
 * catalog-discovered rows coexist, deduplicated by the canonical plugin
 * identity (`name@marketplace`). There is no separate Discover mode and no
 * expansion state switch.
 *
 * The mutation gate is the single fail-closed authority: install requires a
 * ready catalog entry, update/uninstall require an installed Claude-hosted
 * mutable row. Codex-hosted and cache rows have no stable lifecycle adapter in
 * this release and are always unsupported (a visual state, never a fake
 * button).
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

/** One unified row for the single Plugins management list. */
export type PluginsUnifiedRow =
  | { kind: "installed"; plugin: InstalledPluginRow }
  | { kind: "available"; plugin: AvailablePlugin };

export interface PluginsUnifiedView {
  catalogReady: boolean;
  catalogUnavailableCode?: string;
  rows: PluginsUnifiedRow[];
}

/**
 * The unified installed+discovered view from the authoritative plugin
 * inventory. An available catalog entry is dropped when its canonical
 * identity is already installed on this server (the daemon also marks those
 * `installable: false`, but cache rows are re-checked here so one identity
 * renders exactly once). A non-ready catalog keeps installed rows but no
 * install affordances and no discovered rows.
 */
export function pluginsUnifiedView(
  inventory: PluginInventory | undefined,
): PluginsUnifiedView {
  if (!inventory) {
    return { catalogReady: false, rows: [] };
  }
  const catalogReady = inventory.catalog.status === "ready";
  const installedIds = new Set(inventory.installed.map((row) => row.id));
  const rows: PluginsUnifiedRow[] = [...inventory.installed]
    .sort((left, right) => left.name.localeCompare(right.name))
    .map((plugin) => ({ kind: "installed", plugin }));
  if (catalogReady) {
    const available = [...inventory.catalog.available]
      .filter((entry) => entry.installable && !installedIds.has(entry.pluginId))
      .sort((left, right) => left.name.localeCompare(right.name));
    for (const plugin of available) {
      rows.push({ kind: "available", plugin });
    }
  }
  return {
    catalogReady,
    catalogUnavailableCode: catalogReady
      ? undefined
      : inventory.catalog.code,
    rows,
  };
}

/**
 * The authoritative plugin mutation gate. Every supported decision requires
 * daemon-proven capability: a ready catalog entry for install, a mutable
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