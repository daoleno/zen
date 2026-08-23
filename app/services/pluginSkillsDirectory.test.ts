import { describe, expect, test } from "bun:test";
import type { InstalledPluginCopy } from "./pluginsManagement";
import type { LogicalPlugin } from "./pluginsScreenModel";
import type { InstalledSkill } from "./skillsManagement";
import { pluginSkillEntries } from "./pluginSkillsDirectory";

let sequence = 0;
const skill = (overrides: Partial<InstalledSkill> = {}): InstalledSkill => ({
  id: (++sequence).toString(24).padStart(24, "a").slice(-24),
  name: "slack",
  enabled: true,
  rootPath: "/home/test/.codex/plugins/cache/remote/slack/0.1.6/skills/slack",
  canonicalPath:
    "/home/test/.codex/plugins/cache/remote/slack/0.1.6/skills/slack",
  allowedRoot: "/home/test/.codex/plugins/cache/remote/slack/0.1.6",
  location: "Codex managed Plugins",
  scope: "plugin",
  agents: ["codex"],
  capability: { canDelete: false },
  ...overrides,
});

const pluginCopy = (
  overrides: Partial<InstalledPluginCopy> = {},
): InstalledPluginCopy => ({
  copyId: "c".repeat(24),
  pluginId: "slack@remote",
  name: "slack",
  marketplace: "remote",
  scope: "user",
  enabled: true,
  host: "codex",
  source: "remote_cache",
  rootPath: "/home/test/.codex/plugins/cache/remote/slack/0.1.6",
  canonicalPath: "/home/test/.codex/plugins/cache/remote/slack/0.1.6",
  allowedRoot: "/home/test/.codex/plugins/cache/remote/slack",
  location: "Codex managed Plugins",
  revision: "r".repeat(64),
  agents: ["codex"],
  components: [
    { kind: "skill", name: "slack", path: "skills/slack" },
    { kind: "app", name: "Apps", path: ".app.json" },
  ],
  capability: { canUninstall: false },
  version: "0.1.6",
  ...overrides,
});

const plugin = (overrides: Partial<LogicalPlugin> = {}): LogicalPlugin => ({
  key: "slack",
  name: "slack",
  displayName: "Slack",
  copies: [pluginCopy()],
  agents: ["codex"],
  versions: ["0.1.6"],
  canUninstall: false,
  ...overrides,
});

describe("Plugin Skills directory", () => {
  test("lists skill components and resolves the exact inventory copy by path", () => {
    const copy = skill();
    const entries = pluginSkillEntries(plugin(), [copy]);
    expect(entries).toHaveLength(1);
    expect(entries[0]?.name).toBe("slack");
    expect(entries[0]?.path).toBe("skills/slack");
    expect(entries[0]?.copy?.id).toBe(copy.id);
  });

  test("falls back to basename containment for pathless components", () => {
    const copy = skill();
    const logical = plugin({
      copies: [
        pluginCopy({ components: [{ kind: "skill", name: "slack" }] }),
      ],
    });
    expect(pluginSkillEntries(logical, [copy])[0]?.copy?.id).toBe(copy.id);
  });

  test("never matches local or foreign copies", () => {
    const foreign = skill({
      rootPath: "/home/test/.codex/skills/slack",
      scope: "global",
    });
    const outside = skill({
      rootPath: "/srv/other/skills/slack",
      scope: "plugin",
    });
    const entries = pluginSkillEntries(plugin(), [foreign, outside]);
    expect(entries[0]?.copy).toBeUndefined();
  });

  test("keeps one entry per Plugin copy and sorts by name", () => {
    const logical = plugin({
      copies: [
        pluginCopy({
          components: [
            { kind: "skill", name: "beta", path: "skills/beta" },
            { kind: "skill", name: "alpha", path: "skills/alpha" },
            { kind: "skill", name: "alpha", path: "skills/alpha" },
          ],
        }),
        pluginCopy({
          copyId: "d".repeat(24),
          rootPath: "/home/test/.codex/plugins/cache/remote/slack/0.2.0",
          canonicalPath: "/home/test/.codex/plugins/cache/remote/slack/0.2.0",
          allowedRoot: "/home/test/.codex/plugins/cache/remote/slack",
          version: "0.2.0",
          components: [{ kind: "skill", name: "alpha", path: "skills/alpha" }],
        }),
      ],
    });
    const alpha = skill({
      rootPath: "/home/test/.codex/plugins/cache/remote/slack/0.1.6/skills/alpha",
    });
    const beta = skill({
      name: "beta",
      rootPath: "/home/test/.codex/plugins/cache/remote/slack/0.1.6/skills/beta",
    });
    const alphaV2 = skill({
      rootPath: "/home/test/.codex/plugins/cache/remote/slack/0.2.0/skills/alpha",
    });
    const entries = pluginSkillEntries(logical, [beta, alphaV2, alpha]);
    expect(entries.map((entry) => `${entry.key}:${entry.copy?.id ?? "-"}`)).toEqual(
      [
        `${"c".repeat(24)}:alpha:${alpha.id}`,
        `${"d".repeat(24)}:alpha:${alphaV2.id}`,
        `${"c".repeat(24)}:beta:${beta.id}`,
      ],
    );
  });

  test("ignores non-skill components and empty inventories", () => {
    expect(pluginSkillEntries(plugin(), [])).toEqual([
      {
        key: `${"c".repeat(24)}:slack`,
        name: "slack",
        path: "skills/slack",
        copy: undefined,
      },
    ]);
  });
});
