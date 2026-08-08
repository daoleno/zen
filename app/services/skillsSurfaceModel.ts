import type {
  CatalogSkill,
  InstalledSkill,
  ManagedSkillAgent,
  RankedCatalogSkill,
  SkillMutationOperation,
} from "./skillsManagement";
import { skillsRemovalPlanForAgent } from "./skillsScreenModel";

/**
 * First-level Plugins & Skills surface model.
 *
 * Plugins and Skills are separate first-level sections. Both sections read
 * authoritative daemon data: the section switch is pure navigation and never
 * invents data, re-issues requests, or guesses capability.
 *
 * The mutation gate is the single fail-closed authority for action
 * availability. Install and remove are supported only when the daemon's own
 * inventory truth proves them (catalog `installable`, `can_remove` plus an
 * exact per-Agent removal plan, and the daemon's mutation capability list).
 * Update is the official CLI's collection-level operation (update-all in one
 * scope) and is supported only when the daemon's mutation capability list
 * includes it; a per-row Update is never rendered.
 */

export type SkillsSurfaceSection = "plugins" | "skills";

export type SkillsSurfaceAction = {
  type: "select_section";
  section: SkillsSurfaceSection;
};

export interface SkillsSurfaceState {
  section: SkillsSurfaceSection;
}

export type SkillMutationIntent =
  | { kind: "install"; skill: CatalogSkill | RankedCatalogSkill }
  | { kind: "remove"; skill: InstalledSkill; agent: ManagedSkillAgent }
  | { kind: "update"; scope: "project" | "global" };

export type SkillMutationDecision =
  | { supported: true; operation: SkillMutationOperation }
  | { supported: false; reason: string };

export const SKILL_UPDATE_UNSUPPORTED_REASON =
  "Update is not available for Skills on this server.";
export const SKILL_UPDATE_PROJECT_REQUIRES_CWD_REASON =
  "Open a Session with a project directory to update project Skills.";

export function createSkillsSurfaceState(): SkillsSurfaceState {
  return { section: "skills" };
}

export function reduceSkillsSurface(
  current: SkillsSurfaceState,
  action: SkillsSurfaceAction,
): SkillsSurfaceState {
  switch (action.type) {
    case "select_section":
      if (action.section === current.section) {
        return current;
      }
      return { section: action.section };
  }
}

/**
 * Evaluates a mutation intent against authoritative capability truth only.
 * Returns `supported: true` with the exact wire operation when the backend can
 * honor the request; otherwise `supported: false` with the deterministic
 * reason. Callers must never prepare a request for an unsupported intent.
 */
export function evaluateSkillMutation(
  intent: SkillMutationIntent,
  capabilities: readonly SkillMutationOperation[],
): SkillMutationDecision {
  switch (intent.kind) {
    case "install": {
      const skill = intent.skill;
      if (!capabilities.includes("install") || !skill.installable) {
        return {
          supported: false,
          reason: "This catalog identity cannot be installed on this server.",
        };
      }
      return { supported: true, operation: "install" };
    }
    case "remove": {
      const plan = skillsRemovalPlanForAgent(intent.skill, intent.agent);
      if (!capabilities.includes("remove") || !plan) {
        return {
          supported: false,
          reason:
            "No exact removal plan exists for this Skill and Agent on this server.",
        };
      }
      return { supported: true, operation: "remove" };
    }
    case "update":
      if (!capabilities.includes("update")) {
        return { supported: false, reason: SKILL_UPDATE_UNSUPPORTED_REASON };
      }
      return { supported: true, operation: "update" };
  }
}

/** Project-scope update additionally requires a real project directory. */
export function projectUpdateAvailable(projectCwd: string | undefined): boolean {
  return Boolean(projectCwd?.trim());
}
