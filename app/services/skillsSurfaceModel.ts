import type {
  InstalledSkill,
  ManagedSkillAgent,
  RankedCatalogSkill,
  SkillMutationOperation,
  CatalogSkill,
} from "./skillsManagement";

/**
 * First-level Plugins & Skills surface model.
 *
 * Plugins and Skills are separate first-level sections. Both sections read
 * authoritative daemon data: the section switch is pure navigation and never
 * invents data, re-issues requests, or guesses capability.
 *
 * The mutation gate is the single fail-closed authority for action
 * availability: an action renders only when the daemon's own inventory truth
 * proves the operation (the row capability list plus the daemon-wide mutation
 * capability list). Project-scope operations additionally require a real
 * working directory.
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
  | { kind: "import"; skill: CatalogSkill | RankedCatalogSkill; scope: "project" | "global" }
  | { kind: "migrate" }
  | {
      kind: "binding";
      operation: "bind" | "unbind" | "enable" | "disable";
      skill: InstalledSkill;
      agent: ManagedSkillAgent;
      scope: "project" | "global";
    }
  | { kind: "uninstall"; skill: InstalledSkill }
  | { kind: "forget"; skill: InstalledSkill }
  | { kind: "adopt"; skill: InstalledSkill }
  | { kind: "update"; skill: InstalledSkill };

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
  hasProjectCwd = false,
): SkillMutationDecision {
  const operation = intentOperation(intent);
  if (!capabilities.includes(operation)) {
    return {
      supported: false,
      reason: `This server does not support ${operation} for Skills.`,
    };
  }
  switch (intent.kind) {
    case "import": {
      if (!intent.skill.installable) {
        return {
          supported: false,
          reason: "This catalog identity cannot be installed on this server.",
        };
      }
      if (intent.scope === "project" && !hasProjectCwd) {
        return {
          supported: false,
          reason: SKILL_UPDATE_PROJECT_REQUIRES_CWD_REASON,
        };
      }
      return { supported: true, operation: "import" };
    }
    case "migrate":
      return { supported: true, operation: "migrate" };
    case "binding": {
      if (intent.scope === "project" && !hasProjectCwd) {
        return {
          supported: false,
          reason: SKILL_UPDATE_PROJECT_REQUIRES_CWD_REASON,
        };
      }
      return { supported: true, operation: intent.operation };
    }
    case "uninstall":
    case "forget":
    case "adopt":
    case "update":
      return { supported: true, operation };
  }
}

export function intentOperation(
  intent: SkillMutationIntent,
): SkillMutationOperation {
  switch (intent.kind) {
    case "import":
      return "import";
    case "migrate":
      return "migrate";
    case "binding":
      return intent.operation;
    case "uninstall":
      return "uninstall";
    case "forget":
      return "forget";
    case "adopt":
      return "adopt";
    case "update":
      return "update";
  }
}

/** The per-row action set proven by the daemon's own capability list. */
export function skillRowOperations(
  skill: InstalledSkill,
): readonly SkillMutationOperation[] {
  return skill.capability.canManage ? skill.capability.operations : [];
}

export function skillRowSupports(
  skill: InstalledSkill,
  operation: SkillMutationOperation,
  capabilities: readonly SkillMutationOperation[],
): boolean {
  return (
    capabilities.includes(operation) &&
    skill.capability.canManage &&
    skill.capability.operations.includes(operation)
  );
}