/**
 * Plugin request deadlines. The daemon's bounded catalog read must expire
 * before these (defaultPluginCLITimeout = 6s in daemon/skills/plugins.go);
 * the ordering is pinned by the plugins deadline contract tests on both
 * sides.
 */
export const PLUGINS_INVENTORY_TIMEOUT_MS = 10000;
export const PLUGIN_COMMAND_TIMEOUT_MS = 15000;
