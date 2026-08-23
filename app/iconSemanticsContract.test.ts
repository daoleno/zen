import { describe, expect, test } from "bun:test";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const repositoryRoot = join(import.meta.dir, "..");
const productionSourcePaths = execFileSync(
  "git",
  ["ls-files", "-z", "--", "app"],
  { cwd: repositoryRoot, encoding: "utf8" },
)
  .split("\0")
  .filter((path) => /\.(?:js|jsx|ts|tsx)$/.test(path))
  .filter((path) => !/\.(?:test|spec)\.[^.]+$/.test(path));

function source(relativePath: string): string {
  return readFileSync(join(repositoryRoot, relativePath), "utf8");
}

describe("product icon semantics", () => {
  test("generic decorative shine icons stay out of product source", () => {
    const forbiddenIcon = ["spar", "kles?"].join("");
    const offenders = productionSourcePaths.filter((path) =>
      new RegExp(forbiddenIcon, "i").test(source(path)),
    );

    expect(offenders).toEqual([]);
  });

  test("Skills, Model, and delegated Brain origin use semantic icons", () => {
    expect(
      source("app/components/plugins/PluginSkillsDirectory.tsx"),
    ).toContain('name="book-outline"');
    expect(
      source("app/components/plugins/PluginsPresentation.tsx"),
    ).toMatch(/case "skill":\s+return "book-outline";/);
    expect(
      source("app/components/terminal/TerminalActionPopover.tsx"),
    ).toMatch(/key: "model",\s+icon: "hardware-chip-outline"/);
    expect(source("app/components/agents/AgentSessionRow.tsx")).toContain(
      '<Ionicons name="git-network" size={9} color={colors.accentStrong} />',
    );
  });
});
