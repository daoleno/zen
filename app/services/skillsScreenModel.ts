import type {
  InstalledSkill,
  ManagedSkillAgent,
  SkillRemovalPlan,
  SkillsInventory,
} from "./skillsManagement";

export type SkillsLeaderboardView = "all-time" | "trending" | "hot";

export interface SkillsDiscoverState {
  query: string;
  submittedQuery: string;
  view: SkillsLeaderboardView;
}

export type SkillsDiscoverAction =
  | { type: "change_query"; value: string }
  | { type: "submit" }
  | { type: "clear" }
  | { type: "select_view"; view: SkillsLeaderboardView };

export type SkillsDiscoverEffect =
  | { type: "none" }
  | { type: "clear_search" }
  | { type: "submit_search"; query: string };

export interface SkillsDiscoverTransition {
  state: SkillsDiscoverState;
  effect: SkillsDiscoverEffect;
}

export const MANAGED_SKILL_AGENTS: readonly ManagedSkillAgent[] = [
  "codex",
  "claude-code",
  "cursor",
  "opencode",
  "pi",
];

export type SkillsAgentCounts = Record<ManagedSkillAgent, number>;

export interface SkillsAgentProjection {
  agent: ManagedSkillAgent;
  count: number;
  skills: InstalledSkill[];
}

export function createSkillsDiscoverState(): SkillsDiscoverState {
  return { query: "", submittedQuery: "", view: "all-time" };
}

export function reduceSkillsDiscover(
  current: SkillsDiscoverState,
  action: SkillsDiscoverAction,
): SkillsDiscoverTransition {
  switch (action.type) {
    case "change_query": {
      if (!action.value.trim()) {
        return {
          state: { ...current, query: "", submittedQuery: "" },
          effect: current.submittedQuery
            ? { type: "clear_search" }
            : { type: "none" },
        };
      }
      return {
        state: { ...current, query: action.value },
        effect: { type: "none" },
      };
    }
    case "submit": {
      const query = current.query.trim();
      if (query.length < 2) {
        return {
          state: { ...current, submittedQuery: "" },
          effect: current.submittedQuery
            ? { type: "clear_search" }
            : { type: "none" },
        };
      }
      return {
        state: { ...current, query, submittedQuery: query },
        effect: { type: "submit_search", query },
      };
    }
    case "clear":
      return {
        state: { ...current, query: "", submittedQuery: "" },
        effect: current.submittedQuery
          ? { type: "clear_search" }
          : { type: "none" },
      };
    case "select_view":
      return {
        state: { ...current, view: action.view },
        effect: { type: "none" },
      };
  }
}

export function skillsAgentCounts(
  inventory: SkillsInventory | undefined,
): SkillsAgentCounts {
  const counts: SkillsAgentCounts = {
    codex: 0,
    "claude-code": 0,
    cursor: 0,
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
  const projection = skillsAgentProjection(inventory, agent);
  const skills = projection.skills.filter((skill) => skill.manager !== "plugin");
  return { agent, count: skills.length, skills };
}

export function skillsInstallTargets(
  agent: ManagedSkillAgent,
): ManagedSkillAgent[] {
  return [agent];
}

export function skillsRemovalPlanForAgent(
  skill: InstalledSkill,
  agent: ManagedSkillAgent,
): SkillRemovalPlan | null {
  if (!skill.capability.canRemove) {
    return null;
  }
  return (
    skill.capability.removalPlans.find((plan) => plan.agent === agent) ?? null
  );
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
