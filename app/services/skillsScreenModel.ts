import type {
  CatalogSkill,
  InstalledSkill,
  ManagedSkillAgent,
  RankedCatalogSkill,
  SkillsInventory,
} from "./skillsManagement";

export type SkillsLeaderboardView = "all-time" | "trending" | "hot";
export type SkillsStatusFacet = "all" | "enabled" | "disabled" | "available";
export type SkillsScopeFacet = "all" | "global" | "project";
export type SkillsOwnershipFacet = "all" | "zen" | "external" | "catalog";

export interface SkillsFacets {
  status: SkillsStatusFacet;
  scope: SkillsScopeFacet;
  ownership: SkillsOwnershipFacet;
}

export interface SkillsInspectionTarget {
  name: string;
  path?: string;
}

/** Exact package/file identity replayed by the inspector Retry action. */
export function skillsInspectionTarget(
  name: string,
  path?: string,
): SkillsInspectionTarget {
  return path ? { name, path } : { name };
}

export const DEFAULT_SKILLS_FACETS: SkillsFacets = {
  status: "all",
  scope: "all",
  ownership: "all",
};

export const MANAGED_SKILL_AGENTS: readonly ManagedSkillAgent[] = [
  "codex",
  "claude-code",
  "cursor",
  "grok",
  "opencode",
  "pi",
];

export type SkillsAgentCounts = Record<ManagedSkillAgent, number>;

export interface SkillsAgentProjection {
  agent: ManagedSkillAgent;
  count: number;
  skills: InstalledSkill[];
}

/**
 * One unified row for the single Skills management list. Installed and
 * discovered rows coexist: a catalog entry whose canonical identity is already
 * installed for the selected target never renders a duplicate catalog row.
 */
export type SkillsUnifiedRow =
  | { kind: "installed"; skill: InstalledSkill; catalogId: string | null }
  | {
      kind: "catalog";
      skill: CatalogSkill | RankedCatalogSkill;
      catalogId: string;
    };

/**
 * Canonical identity for an installed Skill: source repository plus skill
 * directory name. Builtin/plugin/unknown managers and skills without a
 * provable repository source have no catalog identity and can never dedupe
 * against skills.sh entries.
 */
export function installedSkillCatalogId(skill: InstalledSkill): string | null {
  return skill.source && skill.name ? `${skill.source}/${skill.name}` : null;
}

export function catalogSkillId(
  skill: CatalogSkill | RankedCatalogSkill,
): string {
  return `${skill.source}/${skill.skillId}`;
}

/**
 * Builds the unified Skills list. `catalogSkills` is the current browse
 * source: leaderboard rows when browsing, search rows after a submitted
 * query. Catalog rows that canonically match an installed row are dropped so
 * one identity renders exactly once.
 */
export function skillsUnifiedRows(
  installed: InstalledSkill[],
  catalogSkills: Array<CatalogSkill | RankedCatalogSkill>,
): SkillsUnifiedRow[] {
  const installedRows: SkillsUnifiedRow[] = [...installed]
    .sort((left, right) => left.name.localeCompare(right.name))
    .map((skill) => ({
      kind: "installed",
      skill,
      catalogId: installedSkillCatalogId(skill),
    }));
  const installedIdentities = new Set(
    installedRows
      .map((row) => row.catalogId)
      .filter((id): id is string => id !== null),
  );
  const catalogRows: SkillsUnifiedRow[] = [];
  const seen = new Set<string>();
  for (const skill of catalogSkills) {
    const identity = catalogSkillId(skill);
    if (seen.has(identity) || installedIdentities.has(identity)) {
      continue;
    }
    seen.add(identity);
    catalogRows.push({ kind: "catalog", skill, catalogId: identity });
  }
  return [...installedRows, ...catalogRows];
}

export function skillsAgentCounts(
  inventory: SkillsInventory | undefined,
): SkillsAgentCounts {
  const counts: SkillsAgentCounts = {
    codex: 0,
    "claude-code": 0,
    cursor: 0,
    grok: 0,
    opencode: 0,
    pi: 0,
  };
  for (const skill of inventory?.skills ?? []) {
    for (const agent of MANAGED_SKILL_AGENTS) {
      if (skill.agents.includes(agent)) {
        counts[agent] += 1;
      }
    }
  }
  return counts;
}

/** Counts only Skills that belong on the first-level Skills surface. */
export function skillsSectionAgentCounts(
  inventory: SkillsInventory | undefined,
): SkillsAgentCounts {
  const visibleInventory = inventory
    ? {
        ...inventory,
        skills: inventory.skills.filter((skill) => skill.manager !== "plugin"),
      }
    : undefined;
  return skillsAgentCounts(visibleInventory);
}

export function skillsAgentProjection(
  inventory: SkillsInventory | undefined,
  agent: ManagedSkillAgent,
): SkillsAgentProjection {
  const skills = (inventory?.skills ?? []).filter((skill) =>
    skill.agents.includes(agent),
  );
  return { agent, count: skills.length, skills };
}

/**
 * Skills-section projection: plugin-owned Skills belong to the first-level
 * Plugins section and never appear in the installed Skills list or its Agent
 * counts. Every other installed Skill stays here with its own authoritative
 * management state.
 */
export function skillsSectionProjection(
  inventory: SkillsInventory | undefined,
  agent: ManagedSkillAgent,
): SkillsAgentProjection {
  const skills = (inventory?.skills ?? []).filter(
    (skill) =>
      skill.manager !== "plugin" &&
      (skill.agents.includes(agent) ||
        (skill.owned && skill.bindings.length === 0)),
  );
  return { agent, count: skills.length, skills };
}

export function skillsInstallTargets(
  agent: ManagedSkillAgent,
): ManagedSkillAgent[] {
  return [agent];
}

/** Applies secondary facets without changing authoritative identity/order. */
export function filterSkillsByFacets(
  installed: InstalledSkill[],
  catalog: Array<CatalogSkill | RankedCatalogSkill>,
  facets: SkillsFacets,
): {
  installed: InstalledSkill[];
  catalog: Array<CatalogSkill | RankedCatalogSkill>;
} {
  const installedVisible = installed.filter((skill) => {
    if (facets.status === "available") return false;
    if (facets.status === "enabled" && !skill.enabled) return false;
    if (facets.status === "disabled" && skill.enabled) return false;
    if (facets.scope !== "all" && skill.scope !== facets.scope) return false;
    if (facets.ownership === "zen" && !skill.owned) return false;
    if (facets.ownership === "external" && skill.manager !== "external")
      return false;
    if (facets.ownership === "catalog") return false;
    return true;
  });
  const catalogVisible =
    (facets.status === "all" || facets.status === "available") &&
    facets.scope === "all" &&
    (facets.ownership === "all" || facets.ownership === "catalog")
      ? catalog
      : [];
  return { installed: installedVisible, catalog: catalogVisible };
}

export function skillsLeaderboardLabel(view: SkillsLeaderboardView): string {
  switch (view) {
    case "all-time":
      return "All Time";
    case "trending":
      return "Trending 24h";
    case "hot":
      return "Hot";
  }
}

export function skillsEmptyLeaderboardCopy(view: SkillsLeaderboardView): {
  title: string;
  detail: string;
} {
  return {
    title: `No ${skillsLeaderboardLabel(view)} Skills`,
    detail: "The upstream leaderboard is currently empty.",
  };
}

/**
 * Owns only the automatic refresh identity. Manual refreshes intentionally do
 * not pass through this gate and remain repeatable through request generations.
 */
export class SkillsAutomaticInventoryOwner {
  private identity = "";

  shouldRefresh(
    focusGeneration: number,
    serverId: string | null | undefined,
  ): boolean {
    if (focusGeneration < 1) {
      return false;
    }
    const identity = `${focusGeneration}:${serverId?.trim() || "none"}`;
    if (identity === this.identity) {
      return false;
    }
    this.identity = identity;
    return true;
  }
}
