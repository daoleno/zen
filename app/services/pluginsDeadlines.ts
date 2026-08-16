/**
 * Plugin request deadlines. The daemon's bounded catalog read must expire
 * before these (defaultPluginCLITimeout = 6s in daemon/skills/plugins.go);
 * the ordering is pinned by the plugins deadline contract tests on both
 * sides.
 */
export const PLUGINS_INVENTORY_TIMEOUT_MS = 10000;
export const PLUGIN_COMMAND_TIMEOUT_MS = 15000;
// The App-side mutation read must outlast the daemon's bounded execution
// (DefaultMutationTimeout = 5m plus build time) so a healthy long install or
// update is never cut off by the client. Removals are fast but share this
// ceiling for one simple constant.
export const PLUGIN_MUTATION_TIMEOUT_MS = 540000;
export const SKILLS_MUTATION_TIMEOUT_MS = 540000;
