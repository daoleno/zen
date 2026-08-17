import { describe, expect, test } from "bun:test";
import {
  PLUGINS_SKILLS_MIN_VIEWPORT,
  PLUGINS_SKILLS_SCREEN_PADDING,
  PLUGINS_SKILLS_TOUCH_TARGET,
  compactSkillTargets,
  compactToolbarContentWidth,
  filterAvailablePlugins,
  filterInstalledPlugins,
  filterInstalledSkills,
  installedPluginMetadata,
  installedPluginOwnership,
  installedSkillAvailability,
  installedSkillMetadata,
} from "./pluginsSkillsSurfaceModel";
import type { AvailablePlugin, InstalledPluginRow } from "./pluginsManagement";
import type { InstalledSkill } from "./skillsManagement";

function skill(
  name: string,
  overrides: Partial<InstalledSkill> = {},
): InstalledSkill {
  return {
    id: name.padEnd(24, "0").slice(0, 24),
    name,
    description: `${name} description`,
    enabled: true,
    rootPath: `/home/test/.codex/skills/${name}`,
    canonicalPath: `/home/test/.codex/skills/${name}`,
    allowedRoot: "/home/test/.codex/skills",
    location: "Codex global Skills",
    scope: "global",
    agents: ["codex"],
    capability: { canDelete: true },
    ...overrides,
  };
}

function plugin(
  id: string,
  overrides: Partial<InstalledPluginRow> = {},
): InstalledPluginRow {
  const [name, marketplace] = id.split("@");
  return {
    id,
    name: name!,
    marketplace: marketplace!,
    version: "1.2.3",
    scope: "user",
    enabled: true,
    host: "claude",
    mutable: true,
    source: "catalog",
    skillCount: 1,
    skills: [
      {
        name: `${name}-skill`,
        canonicalPath: `/plugins/${id}/skills/${name}-skill`,
        sourcePath: `/plugins/${id}/skills/${name}-skill`,
      },
    ],
    ...overrides,
  };
}

describe("shared Plugins and Skills presentation model", () => {
  test("compact geometry and all Agent targets remain intact", () => {
    expect(PLUGINS_SKILLS_MIN_VIEWPORT).toBe(360);
    expect(compactToolbarContentWidth(PLUGINS_SKILLS_MIN_VIEWPORT)).toBe(
      PLUGINS_SKILLS_MIN_VIEWPORT - PLUGINS_SKILLS_SCREEN_PADDING * 2,
    );
    expect(PLUGINS_SKILLS_TOUCH_TARGET).toBeGreaterThanOrEqual(44);
    expect(
      compactSkillTargets({
        codex: 1,
        "claude-code": 2,
        cursor: 3,
        grok: 4,
        opencode: 5,
        pi: 6,
      }).map(({ agent, count }) => [agent, count]),
    ).toEqual([
      ["codex", 1],
      ["claude-code", 2],
      ["cursor", 3],
      ["grok", 4],
      ["opencode", 5],
      ["pi", 6],
    ]);
  });

  test("local Skills and Plugins retain their independent search metadata", () => {
    const local = skill("alpha", { description: "Native provider helper" });
    expect(filterInstalledSkills([local], "native provider")).toEqual([local]);
    expect(filterInstalledSkills([local], "remote marketplace")).toEqual([]);

    const installed = plugin("alpha@official-market", {
      skills: [
        {
          name: "hidden-helper",
          canonicalPath: "/plugins/alpha/hidden-helper",
          sourcePath: "/plugins/alpha/hidden-helper",
        },
      ],
    });
    expect(filterInstalledPlugins([installed], "hidden-helper")).toEqual([
      installed,
    ]);
    const available: AvailablePlugin = {
      pluginId: "beta@community-market",
      name: "beta",
      marketplaceName: "community-market",
      sourceRef: "stable-v2",
      installable: true,
    };
    expect(filterAvailablePlugins([available], "stable-v2")).toEqual([
      available,
    ]);
  });

  test("Skill availability metadata remains daemon-derived", () => {
    const local = skill("shared");
    expect(installedSkillMetadata(local)).toBe(
      "Codex global Skills · Global · Codex",
    );
    expect(installedSkillAvailability(local)).toEqual({
      deletable: true,
      summary: "Available to Codex",
      detail: "Delete removes only the copy at Codex global Skills.",
    });
    expect(
      installedSkillAvailability(
        skill("builtin", {
          capability: {
            canDelete: false,
            reason: "Provided by Codex and cannot be deleted from here.",
          },
        }),
      ).detail,
    ).toContain("cannot be deleted from here");
  });

  test("Plugin lifecycle ownership is unchanged by Skills catalog removal", () => {
    const managed = plugin("alpha@official-market");
    expect(installedPluginMetadata(managed)).toBe(
      "Claude Code · @official-market · v1.2.3 · 1 Skill",
    );
    expect(installedPluginOwnership(managed).manageable).toBe(true);

    const codex = plugin("codex@openai-market", {
      host: "codex",
      mutable: false,
      source: "cache",
    });
    expect(installedPluginOwnership(codex)).toEqual({
      manageable: false,
      summary: "Managed by Codex",
      detail:
        "Codex-hosted plugins do not expose a supported lifecycle adapter to Zen.",
    });
    const cached = plugin("cached@official-market", {
      mutable: false,
      source: "cache",
    });
    expect(installedPluginOwnership(cached).summary).toBe(
      "Discovered from client cache",
    );
  });
});
