import type {
  AvailablePlugin,
  InstalledPluginRow,
} from "./pluginsManagement";
import {
  pluginHostLabel,
} from "./pluginsManagement";
import type {
  CatalogSkill,
  InstalledSkill,
  ManagedSkillAgent,
  RankedCatalogSkill,
} from "./skillsManagement";
import {
  scopeLabel,
  skillAgentLabel,
} from "./skillsManagement";
import type { SkillsAgentCounts } from "./skillsScreenModel";
import { MANAGED_SKILL_AGENTS } from "./skillsScreenModel";

/**
 * Layout and presentation truth for the compact Plugins & Skills surface.
 *
 * The UI deliberately has only one first-level navigator (the Plugins and
 * Skills tabs) and one stable management list per tab. Installed and
 * discovered rows coexist with lifecycle badges; search, target, ranking, and
 * refresh are tools inside the selected section, never additional full-width
 * navigation. Keeping the geometry here makes the narrow-phone and large-type
 * contract independently testable.
 */
export const PLUGINS_SKILLS_TOUCH_TARGET = 44;
export const PLUGINS_SKILLS_MIN_VIEWPORT = 360;
export const PLUGINS_SKILLS_SCREEN_PADDING = 16;
export const PLUGINS_SKILLS_CONTROL_GAP = 8;
export const PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER = 1.5;

export interface CompactSkillTarget {
  agent: ManagedSkillAgent;
  label: string;
  count: number;
}

export interface OwnershipPresentation {
  manageable: boolean;
  summary: string;
  detail: string;
}

export interface LifecycleBadge {
  label: string;
  tone: "neutral" | "accent" | "warning";
}

export function compactSkillTargets(
  counts: SkillsAgentCounts,
): CompactSkillTarget[] {
  return MANAGED_SKILL_AGENTS.map((agent) => ({
    agent,
    label: skillAgentLabel(agent),
    count: counts[agent],
  }));
}

export function compactToolbarContentWidth(viewportWidth: number): number {
  return Math.max(
    0,
    viewportWidth - PLUGINS_SKILLS_SCREEN_PADDING * 2,
  );
}

export function filterInstalledSkills(
  skills: InstalledSkill[],
  query: string,
): InstalledSkill[] {
  return filterByQuery(skills, query, (skill) => [
    skill.name,
    skill.description,
    skill.source,
    skill.provenance,
    ownershipLabel(skill),
    scopeLabel(skill.scope),
  ]);
}

export function filterInstalledPlugins(
  plugins: InstalledPluginRow[],
  query: string,
): InstalledPluginRow[] {
  return filterByQuery(plugins, query, (plugin) => [
    plugin.name,
    plugin.marketplace,
    plugin.version,
    pluginHostLabel(plugin.host),
    ...plugin.skills.map((skill) => skill.name),
  ]);
}

export function filterAvailablePlugins(
  plugins: AvailablePlugin[],
  query: string,
): AvailablePlugin[] {
  return filterByQuery(plugins, query, (plugin) => [
    plugin.name,
    plugin.marketplaceName,
    plugin.description,
    plugin.sourceRef,
  ]);
}

export function filterCatalogSkills<T extends CatalogSkill | RankedCatalogSkill>(
  skills: T[],
  query: string,
): T[] {
  return filterByQuery(skills, query, (skill) => [
    skill.name,
    skill.skillId,
    skill.source,
  ]);
}

export function installedSkillMetadata(skill: InstalledSkill): string {
  const source = skill.source || skill.plugin || ownershipLabel(skill);
  return [source, scopeLabel(skill.scope)].filter(Boolean).join(" · ");
}

export function installedSkillBadges(
  skill: InstalledSkill,
  installedCount: number,
): LifecycleBadge[] {
  const badges: LifecycleBadge[] = [];
  if (skill.owned) {
    badges.push({ label: skill.enabled ? "Zen-owned · Enabled" : "Zen-owned · Disabled", tone: skill.enabled ? "accent" : "warning" });
  } else if (skill.tracked) {
    badges.push({ label: "Tracked external", tone: "warning" });
  } else {
    badges.push({ label: "External", tone: "neutral" });
  }
  badges.push({ label: scopeLabel(skill.scope), tone: "neutral" });
  if (installedCount > 1) {
    badges.push({ label: `${installedCount} agents`, tone: "neutral" });
  }
  if (skill.migration === "duplicate" || skill.migration === "conflict") {
    badges.push({ label: skill.migration === "conflict" ? "Conflict" : "Duplicate", tone: "warning" });
  }
  return badges;
}

export function catalogSkillBadges(
  skill: CatalogSkill | RankedCatalogSkill,
  installedForOtherAgents: string[],
): LifecycleBadge[] {
  const badges: LifecycleBadge[] = [{ label: "Available", tone: "neutral" }];
  if (installedForOtherAgents.length > 0) {
    badges.push({
      label: `Installed for ${installedForOtherAgents.join(", ")}`,
      tone: "warning",
    });
  }
  return badges;
}

export function installedSkillOwnership(
  skill: InstalledSkill,
  agent: ManagedSkillAgent,
): OwnershipPresentation {
  if (skill.capability.canManage) {
    if (skill.owned) {
      const binding = skill.bindings.find(
        (candidate) => candidate.agent === agent,
      );
      return {
        manageable: true,
        summary: binding
          ? `Bound · ${scopeLabel(binding.scope)} · ${
              binding.enabled ? "Enabled" : "Disabled"
            }`
          : "In the canonical store",
        detail: `Content lives in Zen's store; bindings are managed per Agent and scope.`,
      };
    }
    return {
      manageable: true,
      summary: "External installation",
      detail:
        skill.capability.reason ||
        "Tracked for adopt/forget. Zen never edits external files unless you adopt.",
    };
  }

  return {
    manageable: false,
    summary: ownershipLabel(skill),
    detail:
      skill.capability.reason ||
      `${ownershipLabel(skill)} owns this Skill outside Zen's management.`,
  };
}

export function installedPluginMetadata(plugin: InstalledPluginRow): string {
  const count = `${plugin.skillCount} ${
    plugin.skillCount === 1 ? "Skill" : "Skills"
  }`;
  return [
    pluginHostLabel(plugin.host),
    `@${plugin.marketplace}`,
    `v${plugin.version}`,
    count,
  ].join(" · ");
}

export function installedPluginBadges(
  plugin: InstalledPluginRow,
): LifecycleBadge[] {
  const badges: LifecycleBadge[] = [
    { label: "Installed", tone: "accent" },
    { label: pluginHostLabel(plugin.host), tone: "neutral" },
  ];
  if (plugin.source === "catalog") {
    badges.push({ label: "Catalog", tone: "neutral" });
  } else {
    badges.push({ label: "Cached", tone: "warning" });
  }
  return badges;
}

export function availablePluginBadges(
  plugin: AvailablePlugin,
): LifecycleBadge[] {
  return [
    { label: "Available", tone: "neutral" },
    { label: `@${plugin.marketplaceName}`, tone: "neutral" },
  ];
}

export function installedPluginOwnership(
  plugin: InstalledPluginRow,
): OwnershipPresentation {
  if (
    plugin.mutable &&
    plugin.host === "claude" &&
    plugin.source === "catalog"
  ) {
    return {
      manageable: true,
      summary: "Managed by Claude Code",
      detail: "Update or uninstall this plugin through its owning client.",
    };
  }
  if (plugin.host === "codex") {
    return {
      manageable: false,
      summary: "Managed by Codex",
      detail: "Codex-hosted plugins do not expose a supported lifecycle adapter to Zen.",
    };
  }
  if (plugin.source === "cache") {
    return {
      manageable: false,
      summary: "Discovered from client cache",
      detail: "This cached plugin can be inspected here but not safely changed from Zen.",
    };
  }
  return {
    manageable: false,
    summary: `Managed by ${pluginHostLabel(plugin.host)}`,
    detail: "The owning client does not currently allow Zen to change this plugin.",
  };
}

export function availablePluginOwnership(
  plugin: AvailablePlugin,
): OwnershipPresentation {
  return {
    manageable: false,
    summary: `Install from @${plugin.marketplaceName}`,
    detail:
      plugin.description ||
      "Installing runs the owning client's official installer for this plugin.",
  };
}

function ownershipLabel(skill: InstalledSkill): string {
  switch (skill.manager) {
    case "zen":
      return "Zen-owned";
    case "external":
      return "External installation";
    case "builtin":
      return "Built in";
    case "plugin":
      return "Plugin owned";
    case "unknown":
      return "External install";
  }
}

function filterByQuery<T>(
  values: T[],
  query: string,
  searchableValues: (value: T) => Array<string | undefined>,
): T[] {
  const normalized = normalizeSearchText(query);
  if (!normalized) {
    return values;
  }
  return values.filter((value) =>
    searchableValues(value).some((candidate) =>
      normalizeSearchText(candidate || "").includes(normalized),
    ),
  );
}

function normalizeSearchText(value: string): string {
  return value.trim().toLocaleLowerCase();
}
