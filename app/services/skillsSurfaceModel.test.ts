import { describe, expect, test } from "bun:test";
import {
  SKILL_UPDATE_PROJECT_REQUIRES_CWD_REASON,
  createSkillsSurfaceState,
  evaluateSkillMutation,
  intentOperation,
  reduceSkillsSurface,
  skillRowOperations,
  skillRowSupports,
} from "./skillsSurfaceModel";
import type { InstalledSkill, SkillMutationOperation } from "./skillsManagement";

const CAPABILITIES: readonly SkillMutationOperation[] = [
  "import",
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

function installedSkill(
  overrides: Partial<InstalledSkill> = {},
): InstalledSkill {
  return {
    id: "aaaaaaaaaaaaaaaaaaaaaaaa",
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
        targetPath: "/home/.codex/skills/demo",
        sourcePath: "/store/demo",
        enabled: true,
        boundAt: "2026-08-01T00:00:00Z",
      },
    ],
    provenance: "Zen canonical store",
    capability: {
      canManage: true,
      operations: [
        "bind",
        "unbind",
        "enable",
        "disable",
        "uninstall",
        "update",
      ],
    },
    ...overrides,
  };
}

describe("surface navigation", () => {
  test("defaults to the Skills section and switches once", () => {
    const initial = createSkillsSurfaceState();
    expect(initial.section).toBe("skills");
    expect(
      reduceSkillsSurface(initial, { type: "select_section", section: "plugins" })
        .section,
    ).toBe("plugins");
    // Idempotent selection returns the same state.
    expect(
      reduceSkillsSurface(initial, { type: "select_section", section: "skills" }),
    ).toBe(initial);
  });
});

describe("evaluateSkillMutation gates every lifecycle action", () => {
  test("import requires the operation, an installable identity, and project cwd", () => {
    const skill = {
      id: "owner/repo/demo",
      skillId: "demo",
      name: "demo",
      source: "owner/repo",
      installs: 10,
      installable: true,
    };
    expect(
      evaluateSkillMutation({ kind: "import", skill, scope: "global" }, CAPABILITIES),
    ).toEqual({ supported: true, operation: "import" });
    const missingOp = evaluateSkillMutation(
      { kind: "import", skill, scope: "global" },
      CAPABILITIES.filter((op) => op !== "import"),
    );
    expect(missingOp.supported).toBe(false);
    expect(intentOperation({ kind: "import", skill, scope: "global" })).toBe("import");

    const projectGate = evaluateSkillMutation(
      { kind: "import", skill, scope: "project" },
      CAPABILITIES,
      false,
    );
    expect(projectGate).toEqual({
      supported: false,
      reason: SKILL_UPDATE_PROJECT_REQUIRES_CWD_REASON,
    });
    expect(
      evaluateSkillMutation(
        { kind: "import", skill, scope: "project" },
        CAPABILITIES,
        true,
      ).supported,
    ).toBe(true);
  });

  test("binding operations are per agent and scope", () => {
    const skill = installedSkill();
    for (const operation of ["bind", "unbind", "enable", "disable"] as const) {
      expect(
        evaluateSkillMutation(
          { kind: "binding", operation, skill, agent: "codex", scope: "global" },
          CAPABILITIES,
        ),
      ).toEqual({ supported: true, operation });
    }
    const projectGate = evaluateSkillMutation(
      { kind: "binding", operation: "bind", skill, agent: "pi", scope: "project" },
      CAPABILITIES,
      false,
    );
    expect(projectGate.supported).toBe(false);
  });

  test("uninstall, forget, adopt, update map to their exact operations", () => {
    const owned = installedSkill();
    const external = installedSkill({ owned: false, tracked: true, manager: "external" });
    const pairs: Array<[Parameters<typeof evaluateSkillMutation>[0], SkillMutationOperation]> = [
      [{ kind: "uninstall", skill: owned }, "uninstall"],
      [{ kind: "forget", skill: external }, "forget"],
      [{ kind: "adopt", skill: external }, "adopt"],
      [{ kind: "update", skill: owned }, "update"],
    ];
    for (const [intent, operation] of pairs) {
      expect(evaluateSkillMutation(intent, CAPABILITIES)).toEqual({
        supported: true,
        operation,
      });
    }
    // A missing capability always fails closed with a deterministic reason.
    const decision = evaluateSkillMutation(
      { kind: "uninstall", skill: owned },
      CAPABILITIES.filter((op) => op !== "uninstall"),
    );
    expect(decision.supported).toBe(false);
    if (decision.supported) {
      throw new Error("expected failure");
    }
    expect(typeof decision.reason).toBe("string");
  });

  test("migrate is supported only when advertised", () => {
    expect(evaluateSkillMutation({ kind: "migrate" }, CAPABILITIES)).toEqual({
      supported: true,
      operation: "migrate",
    });
    const decision = evaluateSkillMutation(
      { kind: "migrate" },
      CAPABILITIES.filter((op) => op !== "migrate"),
    );
    expect(decision.supported).toBe(false);
  });
});

describe("row operation helpers", () => {
  test("skillRowOperations reads the daemon capability list", () => {
    const skill = installedSkill();
    expect(skillRowOperations(skill)).toContain("update");
    expect(skillRowOperations(skill)).not.toContain("adopt");
    const locked = installedSkill({
      capability: { canManage: false, operations: [], reason: "blocked" },
    });
    expect(skillRowOperations(locked)).toEqual([]);
  });

  test("skillRowSupports requires both row and daemon capability", () => {
    const skill = installedSkill();
    expect(skillRowSupports(skill, "update", CAPABILITIES)).toBe(true);
    expect(skillRowSupports(skill, "adopt", CAPABILITIES)).toBe(false);
    expect(
      skillRowSupports(
        skill,
        "update",
        CAPABILITIES.filter((op) => op !== "update"),
      ),
    ).toBe(false);
  });
});
