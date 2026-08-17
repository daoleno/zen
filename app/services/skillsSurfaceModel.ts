import type {
  InstalledSkill,
  SkillMutationOperation,
} from "./skillsManagement";

export type SkillsSurfaceSection = "plugins" | "skills";

export type SkillsSurfaceAction = {
  type: "select_section";
  section: SkillsSurfaceSection;
};

export interface SkillsSurfaceState {
  section: SkillsSurfaceSection;
}

export function createSkillsSurfaceState(): SkillsSurfaceState {
  return { section: "skills" };
}

export function reduceSkillsSurface(
  current: SkillsSurfaceState,
  action: SkillsSurfaceAction,
): SkillsSurfaceState {
  if (action.section === current.section) return current;
  return { section: action.section };
}

export function skillRowSupportsDelete(
  skill: InstalledSkill,
  capabilities: readonly SkillMutationOperation[],
): boolean {
  return capabilities.includes("delete") && skill.capability.canDelete;
}
