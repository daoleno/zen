import { describe, expect, test } from "bun:test";
import {
  PLUGINS_SKILLS_CONTROL_GAP,
  PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER,
  PLUGINS_SKILLS_MIN_VIEWPORT,
  PLUGINS_SKILLS_SCREEN_PADDING,
  PLUGINS_SKILLS_TOUCH_TARGET,
  compactSkillTargets,
  compactToolbarContentWidth,
  filterAvailablePlugins,
  filterCatalogSkills,
  filterInstalledPlugins,
  filterInstalledSkills,
  installedPluginMetadata,
  installedPluginOwnership,
  installedSkillMetadata,
  installedSkillOwnership,
} from "./pluginsSkillsSurfaceModel";
import type {
  AvailablePlugin,
  InstalledPluginRow,
} from "./pluginsManagement";
import type {
  CatalogSkill,
  InstalledSkill,
  ManagedSkillAgent,
} from "./skillsManagement";

function skill(
  name: string,
  overrides: Partial<InstalledSkill> = {},
): InstalledSkill {
  const agents = overrides.agents ?? ["codex"];
  return {
    id: name.padEnd(24, "0").slice(0, 24),
    name,
    description: `${name} description`,
    canonicalPath: `/home/test/.agents/skills/${name}`,
    sourcePath: `/home/test/.agents/skills/${name}`,
    scope: "global",
    agents,
    bindings: [
      {
        sourcePath: `/home/test/.agents/skills/${name}`,
        scope: "global",
        agents,
      },
    ],
    manager: "skills-cli",
    provenance: "official skills-cli lock",
    source: "acme/skills",
    capability: {
      canRemove: true,
      removalPlans: agents
        .filter((agent): agent is ManagedSkillAgent => agent !== "grok")
        .map((agent) => ({ agent, affectedAgents: [agent] })),
    },
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

describe("Plugins & Skills V3 compact geometry", () => {
  test("360dp keeps a bounded content lane and every primary control is at least 44pt", () => {
    expect(PLUGINS_SKILLS_MIN_VIEWPORT).toBe(360);
    expect(compactToolbarContentWidth(PLUGINS_SKILLS_MIN_VIEWPORT)).toBe(328);
    expect(PLUGINS_SKILLS_SCREEN_PADDING).toBe(16);
    expect(PLUGINS_SKILLS_CONTROL_GAP).toBe(8);
    expect(PLUGINS_SKILLS_TOUCH_TARGET).toBeGreaterThanOrEqual(44);
  });

  test("large type is bounded above normal scale without disabling scaling", () => {
    expect(PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER).toBeGreaterThan(1);
    expect(PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER).toBeGreaterThanOrEqual(1.5);
    expect(PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER).toBeLessThanOrEqual(2);
  });

  test("the compact target menu preserves all official client labels and counts", () => {
    const targets = compactSkillTargets({
      codex: 40,
      "claude-code": 75,
      cursor: 0,
      opencode: 3,
      pi: 2,
    });
    expect(targets).toEqual([
      { agent: "codex", label: "Codex", count: 40 },
      { agent: "claude-code", label: "Claude Code", count: 75 },
      { agent: "cursor", label: "Cursor", count: 0 },
      { agent: "opencode", label: "OpenCode", count: 3 },
      { agent: "pi", label: "Pi", count: 2 },
    ]);
  });
});

describe("Plugins & Skills V3 search and filtering", () => {
  test("installed Skills filter by name, source, scope, description, and provenance", () => {
    const values = [
      skill("alpha"),
      skill("beta", {
        source: undefined,
        scope: "builtin",
        manager: "builtin",
        provenance: "Codex builtin",
        description: "Native provider helper",
        capability: { canRemove: false, removalPlans: [] },
      }),
    ];
    expect(filterInstalledSkills(values, "ALPHA")).toEqual([values[0]]);
    expect(filterInstalledSkills(values, "native provider")).toEqual([values[1]]);
    expect(filterInstalledSkills(values, "built in")).toEqual([values[1]]);
    expect(filterInstalledSkills(values, "codex builtin")).toEqual([values[1]]);
    expect(filterInstalledSkills(values, "   ")).toBe(values);
  });

  test("installed and Discover plugins search metadata and included Skills", () => {
    const installed = [
      plugin("alpha@official-market"),
      plugin("beta@community-market", {
        skills: [
          {
            name: "hidden-helper",
            canonicalPath: "/plugins/beta/hidden-helper",
            sourcePath: "/plugins/beta/hidden-helper",
          },
        ],
      }),
    ];
    expect(filterInstalledPlugins(installed, "official")).toEqual([installed[0]]);
    expect(filterInstalledPlugins(installed, "hidden-helper")).toEqual([
      installed[1],
    ]);

    const available: AvailablePlugin[] = [
      {
        pluginId: "alpha@official-market",
        name: "alpha",
        marketplaceName: "official-market",
        description: "Mobile workflows",
        installable: true,
      },
      {
        pluginId: "beta@community-market",
        name: "beta",
        marketplaceName: "community-market",
        sourceRef: "stable-v2",
        installable: true,
      },
    ];
    expect(filterAvailablePlugins(available, "mobile")).toEqual([available[0]]);
    expect(filterAvailablePlugins(available, "stable-v2")).toEqual([
      available[1],
    ]);
  });

  test("catalog Skills stay searchable without new daemon state", () => {
    const catalog: CatalogSkill[] = [
      {
        id: "acme/skills/react-native",
        skillId: "react-native",
        name: "React Native",
        source: "acme/skills",
        installs: 10,
        installable: true,
      },
    ];
    expect(filterCatalogSkills(catalog, "ACME")).toEqual(catalog);
    expect(filterCatalogSkills(catalog, "missing")).toEqual([]);
  });
});

describe("Plugins & Skills V3 quiet ownership presentation", () => {
  test("supported npx Skills expose removal and preserve cross-client impact for inspection", () => {
    const shared = skill("shared", {
      agents: ["codex", "claude-code"],
      capability: {
        canRemove: true,
        removalPlans: [
          {
            agent: "codex",
            affectedAgents: ["codex", "claude-code"],
          },
          {
            agent: "claude-code",
            affectedAgents: ["claude-code"],
          },
        ],
      },
    });
    expect(installedSkillOwnership(shared, "codex")).toEqual({
      manageable: true,
      summary: "Managed with npx skills",
      detail:
        "Installed through the supported Skills manager. Removing it also affects Codex, Claude Code.",
    });
    expect(installedSkillMetadata(shared)).toBe("acme/skills · Global");
  });

  test("unsupported Skill ownership is honest in details without an Unmanaged list badge", () => {
    const builtin = skill("builtin", {
      source: undefined,
      scope: "builtin",
      manager: "builtin",
      provenance: "Codex builtin",
      capability: {
        canRemove: false,
        removalPlans: [],
        reason: "Built-in Skills are owned by Codex.",
      },
    });
    expect(installedSkillMetadata(builtin)).toBe("Built in · Builtin");
    expect(installedSkillOwnership(builtin, "codex")).toEqual({
      manageable: false,
      summary: "Built in",
      detail: "Built-in Skills are owned by Codex.",
    });
  });

  test("Claude catalog plugins expose actions while Codex and cache plugins stay inspect-only", () => {
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
    expect(installedPluginOwnership(cached)).toEqual({
      manageable: false,
      summary: "Discovered from client cache",
      detail:
        "This cached plugin can be inspected here but not safely changed from Zen.",
    });
  });
});
