import type {
  InstalledPluginCopy,
  PluginComponent,
} from "./pluginsManagement";
import type { LogicalPlugin } from "./pluginsScreenModel";
import type { InstalledSkill } from "./skillsManagement";

/**
 * Directory of Skills provided by one installed Plugin.
 *
 * Entries come from daemon-reported Plugin components; an entry is inspectable
 * only when the Skills inventory contains exactly the copy that lives inside
 * this Plugin copy. Ownership is never invented: unmatched entries stay
 * listed without inspection instead of guessing a Skill identity.
 */
export interface PluginSkillEntry {
  /** Stable key: one Plugin copy plus one Skill component. */
  key: string;
  name: string;
  path?: string;
  /** Matching inventory copy; required for inspection. */
  copy?: InstalledSkill;
}

export function pluginSkillEntries(
  plugin: LogicalPlugin,
  skills: readonly InstalledSkill[],
): PluginSkillEntry[] {
  const entries = new Map<string, PluginSkillEntry>();
  for (const copy of plugin.copies) {
    for (const component of copy.components) {
      if (component.kind !== "skill") continue;
      const key = `${copy.copyId}:${component.name}`;
      if (entries.has(key)) continue;
      entries.set(key, {
        key,
        name: component.name,
        path: component.path,
        copy: matchComponentCopy(copy, component, skills),
      });
    }
  }
  return [...entries.values()].sort((left, right) =>
    left.name.localeCompare(right.name),
  );
}

function matchComponentCopy(
  pluginCopy: InstalledPluginCopy,
  component: PluginComponent,
  skills: readonly InstalledSkill[],
): InstalledSkill | undefined {
  if (component.path) {
    const expected = joinPath(pluginCopy.rootPath, component.path);
    const expectedCanonical = joinPath(pluginCopy.canonicalPath, component.path);
    const exact = skills.find(
      (skill) =>
        skill.rootPath === expected || skill.rootPath === expectedCanonical,
    );
    if (exact) return exact;
  }
  return skills.find(
    (skill) =>
      skill.scope === "plugin" &&
      withinRoot(skill.rootPath, pluginCopy.rootPath) &&
      basename(skill.rootPath) === component.name,
  );
}

function joinPath(root: string, relative: string): string {
  return `${root.replace(/\/+$/, "")}/${relative.replace(/^\/+/, "")}`;
}

function withinRoot(path: string, root: string): boolean {
  const normalizedPath = path.replace(/\/+$/, "");
  const normalizedRoot = root.replace(/\/+$/, "");
  return (
    normalizedPath.startsWith(`${normalizedRoot}/`) ||
    normalizedPath === normalizedRoot
  );
}

function basename(path: string): string {
  return path.replace(/\/+$/, "").split("/").pop() ?? "";
}
