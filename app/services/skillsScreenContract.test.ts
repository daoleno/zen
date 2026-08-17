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
  enabled,
  rootPath: `/home/test/.codex/skills/${name}`,
  canonicalPath: `/skills/${name}`,
  allowedRoot: "/home/test/.codex/skills",
  location: "Codex global Skills",
  scope: "global",
  agents: ["codex"],
  capability: { canDelete: true },
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
  test("groups same-name copies without inventing priority or conflicts", () => {
    const codex = skill("imagegen", true, {
      id: "a".repeat(24),
      agents: ["codex"],
      contentHash: "1".repeat(64),
      rootPath: "/home/user/.codex/skills/imagegen",
    });
    const pi = skill("imagegen", true, {
      id: "b".repeat(24),
      agents: ["pi"],
      contentHash: "2".repeat(64),
      rootPath: "/home/user/.pi/agent/skills/imagegen",
      allowedRoot: "/home/user/.pi/agent/skills",
      location: "Pi global Skills",
    });
    const [logical] = groupLogicalSkills([pi, codex]);
    expect(logical?.copies).toHaveLength(2);
    expect(logical?.copies.map((copy) => copy.id)).toEqual([codex.id, pi.id]);
    expect(logical?.agents).toEqual(["codex", "pi"]);
    expect(logical?.primaryCopy.id).toBe(codex.id);
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
        ?.copies.map((copy) => copy.id)
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
  test("keeps Agent membership when filtering disabled copies", () => {
    const disabled = skill("disabled-codex", false, {
      agents: ["codex"],
      contentHash: undefined,
    });
    const [logical] = groupLogicalSkills([disabled]);
    expect(logical?.agents).toEqual(["codex"]);
    expect(logical?.enabled).toBe(false);
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
