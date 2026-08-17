import { describe, expect, test } from "bun:test";
import {
  buildSkillFileTree,
  defaultSkillFile,
  filterLogicalSkills,
  filterInstalledSkills,
  groupLogicalSkills,
  skillRenderer,
} from "./skillsScreenModel";
import type { InstalledSkill, PackageFile } from "./skillsManagement";

const file = (
  path: string,
  kind: PackageFile["kind"] = "text",
  previewStatus: PackageFile["previewStatus"] = "ready",
): PackageFile => ({
  path,
  kind,
  previewStatus,
  size: 10,
  mode: "0600",
  mediaType: kind === "json" ? "application/json" : "text/plain",
});
const skill = (
  name: string,
  enabled = true,
  overrides: Partial<InstalledSkill> = {},
): InstalledSkill => ({
  id: `${name}${overrides.agents?.[0] ?? "codex"}`.padEnd(24, "a").slice(0, 24),
  name,
  description: `${name} local helper`,
  manager: "external",
  owned: false,
  tracked: false,
  enabled,
  canonicalPath: `/skills/${name}`,
  sourcePath: `/skills/${name}`,
  scope: "global",
  agents: ["codex"],
  bindings: [],
  provenance: "Local Agent directory",
  capability: { canManage: true, operations: ["adopt"] },
  ...overrides,
});

describe("local Skills screen model", () => {
  test("search and filters only installed local rows", () => {
    const rows = [skill("alpha"), skill("beta", false)];
    expect(
      filterInstalledSkills(rows, "beta", "all", "all").map(
        (item) => item.name,
      ),
    ).toEqual(["beta"]);
    expect(
      filterInstalledSkills(rows, "", "enabled", "all").map(
        (item) => item.name,
      ),
    ).toEqual(["alpha"]);
  });
  test("merges same-name copies without inventing cross-Agent conflicts", () => {
    const codex = skill("imagegen", true, {
      id: "a".repeat(24),
      agents: ["codex"],
      contentHash: "1".repeat(64),
      sourcePath: "/home/user/.codex/skills/imagegen",
    });
    const pi = skill("imagegen", true, {
      id: "b".repeat(24),
      agents: ["pi"],
      contentHash: "2".repeat(64),
      sourcePath: "/home/user/.pi/agent/skills/imagegen",
    });
    const [logical] = groupLogicalSkills([pi, codex]);
    expect(logical?.copies).toHaveLength(2);
    expect(logical?.enabledByAgent.codex?.map((copy) => copy.id)).toEqual([
      codex.id,
    ]);
    expect(logical?.enabledByAgent.pi?.map((copy) => copy.id)).toEqual([pi.id]);
    expect(logical?.enabledVariantCount).toBe(2);
    expect(logical?.hasConflict).toBe(false);
  });
  test("keeps global and project copies without inventing scope priority", () => {
    const global = skill("review", true, {
      id: "c".repeat(24),
      agents: ["codex"],
      scope: "global",
    });
    const project = skill("review", true, {
      id: "d".repeat(24),
      agents: ["codex"],
      scope: "project",
    });
    const rows = groupLogicalSkills([
      global,
      project,
      skill("pi-only", true, { agents: ["pi"] }),
    ]);
    expect(
      rows
        .find((row) => row.name === "review")
        ?.enabledByAgent.codex?.map((copy) => copy.id)
        .sort(),
    ).toEqual([global.id, project.id].sort());
    expect(
      filterLogicalSkills(rows, "", {
        agents: ["pi"],
        status: "enabled",
        scope: "all",
      }).map((row) => row.name),
    ).toEqual(["pi-only"]);
    expect(
      filterLogicalSkills(rows, "review", {
        agents: [],
        status: "all",
        scope: "project",
      }).map((row) => row.name),
    ).toEqual(["review"]);
  });
  test("keeps disabled Agent membership without inventing enabled content variants", () => {
    const disabled = skill("disabled-codex", false, {
      agents: ["codex"],
      bindings: [
        {
          agent: "codex",
          scope: "global",
          mode: "direct",
          targetPath: "/skills/disabled-codex",
          sourcePath: "/skills/disabled-codex",
          enabled: false,
          boundAt: "",
          operations: [],
        },
      ],
      contentHash: undefined,
    });
    const [logical] = groupLogicalSkills([disabled]);
    expect(logical?.installedAgents).toEqual(["codex"]);
    expect(logical?.agents).toEqual([]);
    expect(logical?.enabledVariantCount).toBe(0);
    expect(
      filterLogicalSkills([logical!], "", {
        agents: ["codex"],
        status: "disabled",
        scope: "all",
      }),
    ).toHaveLength(1);
  });
  test("tree orders directories first and files by locale name", () => {
    const tree = buildSkillFileTree([
      file("z.txt"),
      file("config/z.yaml"),
      file("config/a.json", "json"),
      file("A.md", "markdown"),
    ]);
    expect(tree.map((node) => `${node.kind}:${node.name}`)).toEqual([
      "directory:config",
      "file:A.md",
      "file:z.txt",
    ]);
    expect(tree[0]!.children.map((node) => node.name)).toEqual([
      "a.json",
      "z.yaml",
    ]);
  });
  test("SKILL.md is the default, then the first non-binary file", () => {
    expect(
      defaultSkillFile([
        file("image.bin", "binary", "binary"),
        file("notes.txt"),
        file("SKILL.md", "markdown"),
      ]),
    ).toBe("SKILL.md");
    expect(
      defaultSkillFile([
        file("image.bin", "binary", "binary"),
        file("notes.txt"),
      ]),
    ).toBe("notes.txt");
  });
  test("renderer selection exposes invalid JSON and binary states", () => {
    expect(skillRenderer("markdown", "# Hi")).toBe("markdown");
    expect(skillRenderer("json", '{"ok":true}')).toBe("json");
    expect(skillRenderer("json", "{")).toBe("invalid-json");
    expect(skillRenderer("binary")).toBe("binary");
  });
});
