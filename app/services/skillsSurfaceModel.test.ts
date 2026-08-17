import { describe, expect, test } from "bun:test";
import {
  createSkillsSurfaceState,
  reduceSkillsSurface,
  skillRowSupportsDelete,
} from "./skillsSurfaceModel";
import type { InstalledSkill } from "./skillsManagement";

function skill(canDelete: boolean): InstalledSkill {
  return {
    id: "a".repeat(24),
    name: "demo",
    enabled: true,
    rootPath: "/home/test/.codex/skills/demo",
    canonicalPath: "/home/test/.codex/skills/demo",
    allowedRoot: "/home/test/.codex/skills",
    location: "Codex global Skills",
    scope: "global",
    agents: ["codex"],
    capability: canDelete
      ? { canDelete: true }
      : { canDelete: false, reason: "Provided by Codex and cannot be deleted from here." },
  };
}

describe("local Skills action gates", () => {
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

  test("delete requires both copy and daemon capability", () => {
    expect(skillRowSupportsDelete(skill(true), ["delete"])).toBe(true);
    expect(skillRowSupportsDelete(skill(true), [])).toBe(false);
    expect(skillRowSupportsDelete(skill(false), ["delete"])).toBe(false);
  });
});
