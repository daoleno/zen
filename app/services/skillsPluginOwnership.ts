import type { InstalledSkill } from "./skillsManagement";
import type { LogicalPlugin } from "./pluginsScreenModel";

/**
 * Derived ownership for plugin-provided Skills.
 *
 * Ownership is never invented by the UI: a Skill copy is attributed to a
 * Plugin only when daemon-reported evidence (the copy's `plugin` field, or a
 * `scope: "plugin"` root path contained inside an installed Plugin copy)
 * matches exactly one entry of the Plugins inventory. Everything else stays
 * unattributed rather than guessed.
 */
export interface SkillPluginOwner {
  /** LogicalPlugin.key of the owning Plugin. */
  key: string;
  name: string;
  displayName: string;
  description?: string;
  match: "plugin-name" | "plugin-path";
}

export function resolveSkillCopyPluginOwner(
  copy: InstalledSkill,
  plugins: readonly LogicalPlugin[],
): SkillPluginOwner | null {
  const declared = normalizePluginReference(copy.plugin);
  if (declared) {
    const byName = findPlugin(plugins, (candidate) =>
      matchesPluginName(candidate, declared),
    );
    if (byName) return owner(byName, "plugin-name");
  }
  if (copy.scope !== "plugin") return null;
  const byPath = findPlugin(plugins, (candidate) =>
    containsPluginCopyRoot(candidate, copy.rootPath),
  );
  return byPath ? owner(byPath, "plugin-path") : null;
}

/**
 * Skills list projection for the Skills surface: copies owned by an installed
 * Plugin are presented once, inside that Plugin's expandable Skills directory,
 * so the list never duplicates Plugin-owned content. Attribution stays
 * evidence-based; when no owner matches, the copy remains listed here.
 */
export function skillsOutsidePlugins(
  skills: readonly InstalledSkill[],
  plugins: readonly LogicalPlugin[],
): InstalledSkill[] {
  return skills.filter(
    (copy) => resolveSkillCopyPluginOwner(copy, plugins) === null,
  );
}

export function skillPluginStatusReason(
  copy: InstalledSkill,
  owner: SkillPluginOwner | null,
): string {
  if (copy.capability.reason) return copy.capability.reason;
  if (owner) {
    return `${owner.displayName} provides and manages this Skill. Manage or remove it through the Plugin.`;
  }
  if (copy.scope === "builtin") {
    return "This Skill ships with the Agent itself and cannot be deleted here.";
  }
  return "This Skill is managed outside this screen and cannot be deleted here.";
}

function owner(
  plugin: LogicalPlugin,
  match: SkillPluginOwner["match"],
): SkillPluginOwner {
  return {
    key: plugin.key,
    name: plugin.name,
    displayName: plugin.displayName,
    description: plugin.description,
    match,
  };
}

function findPlugin(
  plugins: readonly LogicalPlugin[],
  predicate: (candidate: LogicalPlugin) => boolean,
): LogicalPlugin | null {
  return plugins.find(predicate) ?? null;
}

function normalizePluginReference(value: string | undefined): string | null {
  const trimmed = value?.trim().toLocaleLowerCase() ?? "";
  return trimmed || null;
}

function matchesPluginName(
  plugin: LogicalPlugin,
  reference: string,
): boolean {
  const name = plugin.name.toLocaleLowerCase();
  if (name === reference || plugin.key === reference) return true;
  // Daemon references may be fully qualified ("name@marketplace"); the
  // inventory keys plugins by their plain name.
  const bare = reference.split("@")[0] ?? "";
  return Boolean(bare) && bare === name;
}

function containsPluginCopyRoot(
  plugin: LogicalPlugin,
  skillRootPath: string,
): boolean {
  const skillRoot = trimSlashes(skillRootPath);
  if (!skillRoot) return false;
  return plugin.copies.some((copyCandidate) => {
    const roots = [copyCandidate.rootPath, copyCandidate.canonicalPath];
    return roots.some((root) => {
      const normalized = trimSlashes(root);
      return (
        Boolean(normalized) &&
        (skillRoot === normalized ||
          skillRoot.startsWith(`${normalized}/`))
      );
    });
  });
}

function trimSlashes(value: string): string {
  return value.trim().replace(/\/+$/, "");
}
