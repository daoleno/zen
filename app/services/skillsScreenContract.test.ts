import { describe, expect, test } from "bun:test";
import {
  buildSkillFileTree,
  defaultSkillFile,
  filterInstalledSkills,
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
const skill = (name: string, enabled = true): InstalledSkill => ({
  id: name.padEnd(24, "a").slice(0, 24),
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
