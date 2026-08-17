import { describe, expect, test } from "bun:test";
import {
  createSkillsSurfaceState,
  evaluateSkillMutation,
  reduceSkillsSurface,
  skillBindingSupports,
  skillRowSupports,
} from "./skillsSurfaceModel";
import type {
  InstalledSkill,
  SkillMutationOperation,
} from "./skillsManagement";

const capabilities: SkillMutationOperation[] = [
  "migrate",
  "bind",
  "unbind",
  "enable",
  "disable",
  "uninstall",
  "forget",
  "adopt",
  "update",
];
const skill = {
  id: "a".repeat(24),
  name: "demo",
  manager: "zen",
  owned: true,
  tracked: true,
  enabled: true,
  canonicalPath: "/store/demo",
  sourcePath: "/store/demo",
  scope: "global",
  agents: ["codex"],
  bindings: [
    {
      agent: "codex",
      scope: "global",
      mode: "symlink",
      targetPath: "/target",
      sourcePath: "/store/demo",
      enabled: true,
      operations: ["disable", "unbind"],
    },
  ],
  provenance: "Zen",
  capability: {
    canManage: true,
    operations: ["bind", "disable", "unbind", "uninstall", "update"],
  },
} as InstalledSkill;

describe("local Skills lifecycle gates", () => {
  test("Plugins and Skills remain separate top-level sections", () => {
    const initial = createSkillsSurfaceState();
    expect(initial.section).toBe("skills");
    expect(
      reduceSkillsSurface(initial, {
        type: "select_section",
        section: "plugins",
      }).section,
    ).toBe("plugins");
  });
  test("only daemon-advertised local lifecycle actions pass", () => {
    expect(
      evaluateSkillMutation({ kind: "update", skill }, capabilities),
    ).toEqual({ supported: true, operation: "update" });
    expect(
      evaluateSkillMutation(
        { kind: "update", skill },
        capabilities.filter((item) => item !== "update"),
      ).supported,
    ).toBe(false);
    expect(evaluateSkillMutation({ kind: "migrate" }, capabilities)).toEqual({
      supported: true,
      operation: "migrate",
    });
  });
  test("row and binding helpers fail closed", () => {
    expect(skillRowSupports(skill, "update", capabilities)).toBe(true);
    expect(skillRowSupports(skill, "adopt", capabilities)).toBe(false);
    expect(
      skillBindingSupports(skill.bindings[0]!, "disable", capabilities),
    ).toBe(true);
    expect(
      skillBindingSupports(skill.bindings[0]!, "enable", capabilities),
    ).toBe(false);
  });
});
