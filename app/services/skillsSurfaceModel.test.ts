import { describe, expect, test } from "bun:test";
import {
  SKILL_UPDATE_UNSUPPORTED_REASON,
  createSkillsSurfaceState,
  evaluateSkillMutation,
  projectUpdateAvailable,
  reduceSkillsSurface,
} from "./skillsSurfaceModel";
import {
  skillsSectionAgentCounts,
  skillsSectionProjection,
} from "./skillsScreenModel";
import type {
  CatalogSkill,
  InstalledSkill,
  SkillMutationOperation,
  SkillsInventory,
} from "./skillsManagement";

const FULL_CAPABILITIES: readonly SkillMutationOperation[] = [
  "install",
  "remove",
  "update",
];
const LEGACY_CAPABILITIES: readonly SkillMutationOperation[] = [
  "install",
  "remove",
];

function installedSkill(
  id: string,
  name: string,
  manager: InstalledSkill["manager"] = "skills-cli",
  removable = false,
): InstalledSkill {
  return {
    id,
    name,
    canonicalPath: `/home/test/.agents/skills/${name}`,
    sourcePath: `/home/test/.agents/skills/${name}`,
    scope: "global",
    agents: ["codex"],
    bindings: [
      {
        sourcePath: `/home/test/.agents/skills/${name}`,
        scope: "global",
        agents: ["codex"],
      },
    ],
    manager,
    provenance:
      manager === "skills-cli" ? "official skills-cli lock" : "unknown",
    source: manager === "skills-cli" ? "acme/skills" : undefined,
    capability: removable
      ? {
          canRemove: true,
          removalPlans: [{ agent: "codex", affectedAgents: ["codex"] }],
        }
      : {
          canRemove: false,
          removalPlans: [],
          reason: "No official skills-cli provenance proves a safe management command.",
        },
  };
}

function pluginSkill(id: string, name: string): InstalledSkill {
  return {
    ...installedSkill(id, name, "plugin"),
    plugin: "sample-plugin",
    scope: "plugin",
  };
}

function inventory(skills: InstalledSkill[]): SkillsInventory {
  return {
    generatedAt: "2026-08-08T00:00:00Z",
    skills,
    agents: [],
    warnings: [],
    mutationOperations: [...FULL_CAPABILITIES],
  };
}

function catalogSkill(installable: boolean): CatalogSkill {
  return {
    id: "acme/skills/good",
    skillId: "good",
    name: "good",
    source: "acme/skills",
    installs: 10,
    installable,
  };
}

describe("Plugins & Skills first-level sections", () => {
  test("section switch is pure navigation and never duplicates state", () => {
    let state = createSkillsSurfaceState();
    expect(state.section).toBe("skills");

    state = reduceSkillsSurface(state, {
      type: "select_section",
      section: "plugins",
    });
    expect(state.section).toBe("plugins");

    state = reduceSkillsSurface(state, {
      type: "select_section",
      section: "plugins",
    });
    expect(state.section).toBe("plugins");

    state = reduceSkillsSurface(state, {
      type: "select_section",
      section: "skills",
    });
    expect(state.section).toBe("skills");
  });

  test("Skills section projection excludes plugin-owned Skills and counts", () => {
    const projection = skillsSectionProjection(
      inventory([
        installedSkill("000000000000000000000001", "cli-skill", "skills-cli", true),
        installedSkill("000000000000000000000002", "builtin-skill", "builtin"),
        pluginSkill("000000000000000000000003", "hosted-skill"),
      ]),
      "codex",
    );
    expect(projection.count).toBe(2);
    expect(projection.skills.map((skill) => skill.name)).toEqual([
      "cli-skill",
      "builtin-skill",
    ]);
    expect(
      skillsSectionAgentCounts(
        inventory([
          installedSkill("000000000000000000000001", "cli-skill", "skills-cli", true),
          installedSkill("000000000000000000000002", "builtin-skill", "builtin"),
          pluginSkill("000000000000000000000003", "hosted-skill"),
        ]),
      ).codex,
    ).toBe(2);
  });
});

describe("Skill mutation gate", () => {
  test("install is supported only for catalog-truthful installable identities", () => {
    expect(
      evaluateSkillMutation(
        { kind: "install", skill: catalogSkill(true) },
        FULL_CAPABILITIES,
      ),
    ).toEqual({ supported: true, operation: "install" });

    const decision = evaluateSkillMutation(
      { kind: "install", skill: catalogSkill(false) },
      FULL_CAPABILITIES,
    );
    expect(decision.supported).toBe(false);
  });

  test("remove is supported only when an exact Agent removal plan exists", () => {
    const removable = installedSkill(
      "000000000000000000000011",
      "removable",
      "skills-cli",
      true,
    );
    expect(
      evaluateSkillMutation(
        { kind: "remove", skill: removable, agent: "codex" },
        FULL_CAPABILITIES,
      ),
    ).toEqual({ supported: true, operation: "remove" });

    const unmanaged = installedSkill("000000000000000000000012", "unmanaged");
    expect(
      evaluateSkillMutation(
        { kind: "remove", skill: unmanaged, agent: "codex" },
        FULL_CAPABILITIES,
      ),
    ).toEqual({
      supported: false,
      reason:
        "No exact removal plan exists for this Skill and Agent on this server.",
    });
  });

  test("update is a collection-level capability gated by the daemon mutation list", () => {
    expect(
      evaluateSkillMutation({ kind: "update", scope: "global" }, FULL_CAPABILITIES),
    ).toEqual({ supported: true, operation: "update" });
    expect(
      evaluateSkillMutation({ kind: "update", scope: "project" }, FULL_CAPABILITIES),
    ).toEqual({ supported: true, operation: "update" });

    expect(
      evaluateSkillMutation(
        { kind: "update", scope: "global" },
        LEGACY_CAPABILITIES,
      ),
    ).toEqual({
      supported: false,
      reason: SKILL_UPDATE_UNSUPPORTED_REASON,
    });
  });

  test("legacy capability lists keep install and remove working", () => {
    expect(
      evaluateSkillMutation(
        { kind: "install", skill: catalogSkill(true) },
        LEGACY_CAPABILITIES,
      ),
    ).toEqual({ supported: true, operation: "install" });
    const removable = installedSkill(
      "000000000000000000000021",
      "removable",
      "skills-cli",
      true,
    );
    expect(
      evaluateSkillMutation(
        { kind: "remove", skill: removable, agent: "codex" },
        LEGACY_CAPABILITIES,
      ),
    ).toEqual({ supported: true, operation: "remove" });
  });

  test("project update requires a real project directory", () => {
    expect(projectUpdateAvailable("/workspace/project")).toBe(true);
    expect(projectUpdateAvailable("")).toBe(false);
    expect(projectUpdateAvailable(undefined)).toBe(false);
    expect(projectUpdateAvailable("   ")).toBe(false);
  });
});
