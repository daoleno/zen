import { describe, expect, test } from "bun:test";
import type { InstalledSkill } from "./skillsManagement";
import type { InstalledPluginCopy, PluginHost } from "./pluginsManagement";
import type { LogicalPlugin } from "./pluginsScreenModel";
import {
  resolveSkillCopyPluginOwner,
  skillPluginStatusReason,
  skillsOutsidePlugins,
} from "./skillsPluginOwnership";

let skillSequence = 0;
const skill = (overrides: Partial<InstalledSkill> = {}): InstalledSkill => ({
  id: (++skillSequence).toString(24).padStart(24, "a").slice(-24),
  name: "brand-kit",
  enabled: true,
  rootPath: "/home/test/.claude/plugins/design/skills/brand-kit",
  canonicalPath: "/home/test/.claude/plugins/design/skills/brand-kit",
  allowedRoot: "/home/test/.claude/plugins",
  location: "Claude Code plugin cache",
  scope: "plugin",
  agents: ["claude-code"],
  capability: { canDelete: false },
  ...overrides,
});

const pluginCopy = (
  overrides: Partial<InstalledPluginCopy> = {},
): InstalledPluginCopy => ({
  copyId: "c".repeat(24),
  pluginId: "design@zen-plugins",
  name: "design",
  marketplace: "zen-plugins",
  scope: "user",
  enabled: true,
  host: "claude" as PluginHost,
  source: "manager",
  rootPath: "/home/test/.claude/plugins/design",
  canonicalPath: "/home/test/.claude/plugins/design",
  allowedRoot: "/home/test/.claude/plugins",
  location: "Claude Code plugins",
  revision: "r".repeat(64),
  agents: ["claude-code"],
  components: [],
  capability: { canUninstall: true },
  version: "1.0.0",
  ...overrides,
});

const plugin = (overrides: Partial<LogicalPlugin> = {}): LogicalPlugin => ({
  key: "design",
  name: "design",
  displayName: "Design Kit",
  copies: [pluginCopy()],
  agents: ["claude-code"],
  versions: ["1.0.0"],
  canUninstall: true,
  ...overrides,
});

describe("Skill plugin ownership", () => {
  test("attributes a locked Skill by its daemon-declared plugin name", () => {
    const owner = resolveSkillCopyPluginOwner(
      skill({ plugin: "design@zen-plugins" }),
      [plugin()],
    );
    expect(owner?.key).toBe("design");
    expect(owner?.displayName).toBe("Design Kit");
    expect(owner?.match).toBe("plugin-name");
  });

  test("matches case-insensitively without inventing owners", () => {
    const owner = resolveSkillCopyPluginOwner(skill({ plugin: "Design" }), [
      plugin(),
    ]);
    expect(owner?.key).toBe("design");
    expect(
      resolveSkillCopyPluginOwner(
        skill({
          plugin: "unrelated",
          rootPath: "/srv/local-skills/unrelated",
          canonicalPath: "/srv/local-skills/unrelated",
        }),
        [plugin()],
      ),
    ).toBeNull();
  });

  test("falls back to plugin path containment for plugin-scoped Skills", () => {
    const owner = resolveSkillCopyPluginOwner(skill({ plugin: undefined }), [
      plugin(),
    ]);
    expect(owner?.key).toBe("design");
    expect(owner?.match).toBe("plugin-path");
    expect(
      resolveSkillCopyPluginOwner(
        skill({
          plugin: undefined,
          rootPath: "/home/test/.codex/skills/other",
        }),
        [plugin()],
      ),
    ).toBeNull();
  });

  test("never attributes local or builtin Skills", () => {
    expect(
      resolveSkillCopyPluginOwner(
        skill({ scope: "global", plugin: undefined }),
        [plugin()],
      ),
    ).toBeNull();
    expect(
      resolveSkillCopyPluginOwner(
        skill({ scope: "builtin", plugin: undefined }),
        [plugin()],
      ),
    ).toBeNull();
    expect(resolveSkillCopyPluginOwner(skill(), [])).toBeNull();
  });

  test("status reasons prefer daemon explanations over UI prose", () => {
    const owner = resolveSkillCopyPluginOwner(skill({ plugin: "design" }), [
      plugin(),
    ])!;
    expect(
      skillPluginStatusReason(skill({ capability: { canDelete: false } }), null),
    ).toContain("cannot be deleted here.");
    expect(
      skillPluginStatusReason(
        skill({
          scope: "builtin",
          capability: { canDelete: false },
        }),
        null,
      ),
    ).toContain("ships with the Agent");
    expect(
      skillPluginStatusReason(
        skill({
          capability: {
            canDelete: false,
            reason: "Provided by plugin design",
          },
        }),
        owner,
      ),
    ).toBe("Provided by plugin design");
  });

  test("Skills list keeps only copies no installed Plugin owns", () => {
    const owned = skill({ plugin: "design@zen-plugins" });
    const local = skill({
      name: "notes",
      rootPath: "/home/test/.claude/skills/notes",
      canonicalPath: "/home/test/.claude/skills/notes",
      scope: "global",
      plugin: undefined,
    });
    const unattributed = skill({
      plugin: "ghost@nowhere",
      rootPath: "/srv/isolated/brand-kit",
      canonicalPath: "/srv/isolated/brand-kit",
    });
    const listed = skillsOutsidePlugins([owned, local, unattributed], [
      plugin(),
    ]);
    expect(listed.map((copy) => copy.id)).toEqual([local.id, unattributed.id]);
    expect(skillsOutsidePlugins([owned], [])).toEqual([owned]);
  });
});
