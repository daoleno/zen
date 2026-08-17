import { describe, expect, test } from "bun:test";
import {
  MOBILE_SINGLE_LINE_INPUT_LAYOUT,
  mobileSingleLineScaledLineHeight,
  mobileSingleLineTextLaneWidth,
} from "../components/ui/mobileSingleLineInputModel";
import {
  SkillsAutomaticInventoryOwner,
  catalogSkillId,
  installedSkillCatalogId,
  skillsAgentCounts,
  skillsAgentProjection,
  skillsSectionProjection,
  skillsEmptyLeaderboardCopy,
  filterSkillsByFacets,
  skillsInstallTargets,
  skillsInspectionTarget,
  skillsUnifiedRows,
} from "./skillsScreenModel";
import type {
  CatalogSkill,
  InstalledSkill,
  ManagedSkillAgent,
  RankedCatalogSkill,
  SkillsInventory,
} from "./skillsManagement";
import { readFileSync } from "node:fs";

function installedSkill(
  id: string,
  name: string,
  agents: ManagedSkillAgent[],
  overrides: Partial<InstalledSkill> = {},
): InstalledSkill {
  return {
    id,
    name,
    manager: "zen",
    owned: true,
    tracked: true,
    enabled: true,
    canonicalPath: `/store/${name}`,
    sourcePath: `/store/${name}`,
    scope: "global",
    agents,
    bindings: agents.map((agent) => ({
      agent,
      scope: "global" as const,
      mode: "symlink" as const,
      targetPath: `/agent/${agent}/skills/${name}`,
      sourcePath: `/store/${name}`,
      enabled: true,
      boundAt: "2026-08-01T00:00:00Z",
      operations: ["unbind", "disable"],
    })),
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

function inventory(skills: InstalledSkill[]): SkillsInventory {
  return {
    generatedAt: "2026-07-20T00:00:00Z",
    skills,
    agents: [],
    warnings: [],
    mutationOperations: [
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
    ],
    migration: { owned: 0, external: 0, duplicate: 0, conflict: 0, tracked: 0 },
  };
}

function catalogSkill(
  name: string,
  source: string,
  installable = true,
): CatalogSkill {
  return {
    id: `${source}/${name}`,
    skillId: name,
    name,
    installs: 120,
    source,
    installable,
  };
}

describe("Skills screen behavior", () => {
  test("native-stack header owns the top inset so tabs cannot regain a spacer", () => {
    const presentation = readFileSync(
      new URL("../components/skills/SkillsPresentation.tsx", import.meta.url),
      "utf8",
    );
    const layout = readFileSync(
      new URL("../app/_layout.tsx", import.meta.url),
      "utf8",
    );
    const skillsRoute = layout.slice(
      layout.indexOf('<Stack.Screen\n        name="skills"'),
      layout.indexOf('<Stack.Screen\n        name="stats"'),
    );
    const rootStart = presentation.indexOf(
      '<SafeAreaView edges={["left", "right"]} style={styles.root}>',
    );
    const tabsStart = presentation.indexOf(
      "<SurfaceTabs section={section} onSelect={onSelectSection} />",
      rootStart,
    );
    const tabsStyle = presentation.slice(
      presentation.indexOf("  surfaceTabs: {"),
      presentation.indexOf("  surfaceTab: {"),
    );

    expect(skillsRoute).toContain('title: "Skills"');
    expect(skillsRoute).not.toContain("headerShown: false");
    expect(rootStart).toBeGreaterThan(-1);
    expect(presentation).not.toContain('<SafeAreaView edges={["top"]}');
    expect(presentation.slice(rootStart, tabsStart).trim()).toBe(
      '<SafeAreaView edges={["left", "right"]} style={styles.root}>',
    );
    expect(tabsStyle).not.toMatch(/paddingTop|marginTop/);
  });

  test("automatic inventory is exactly once per focus and canonical server identity", () => {
    const owner = new SkillsAutomaticInventoryOwner();

    expect(owner.shouldRefresh(0, "server-a")).toBe(false);
    expect(owner.shouldRefresh(1, "server-a")).toBe(true);
    expect(owner.shouldRefresh(1, "server-a")).toBe(false);

    for (let index = 0; index < 20; index += 1) {
      expect(owner.shouldRefresh(1, "server-a")).toBe(false);
    }

    expect(owner.shouldRefresh(1, "server-b")).toBe(true);
    expect(owner.shouldRefresh(1, "server-b")).toBe(false);
    expect(owner.shouldRefresh(2, "server-b")).toBe(true);
  });

  test("pull-to-refresh remains repeatable outside the automatic focus gate", () => {
    const owner = new SkillsAutomaticInventoryOwner();
    let requests = 0;
    const pullToRefresh = () => {
      requests += 1;
    };

    if (owner.shouldRefresh(1, "server-a")) requests += 1;
    pullToRefresh();
    pullToRefresh();

    expect(requests).toBe(3);
    expect(owner.shouldRefresh(1, "server-a")).toBe(false);
  });

  test("inspector Retry preserves the exact package or selected file identity", () => {
    expect(skillsInspectionTarget("demo")).toEqual({ name: "demo" });
    expect(skillsInspectionTarget("demo", "references/a.md")).toEqual({
      name: "demo",
      path: "references/a.md",
    });
  });

  test("unified rows keep installed and discovered together, deduplicated by canonical identity", () => {
    const installed = [
      installedSkill("000000000000000000000001", "codex-only", ["codex"], {
        source: "owner/repo-a",
      }),
      installedSkill("000000000000000000000002", "builtin-one", ["codex"], {
        manager: "builtin",
        owned: false,
        tracked: false,
        capability: { canManage: false, operations: [], reason: "Builtin." },
      }),
    ];
    const catalog = [
      catalogSkill("codex-only", "owner/repo-a"),
      catalogSkill("fresh-skill", "owner/repo-b"),
      catalogSkill("fresh-skill", "owner/repo-b"),
      catalogSkill("another", "owner/repo-c", false),
    ];
    const rows = skillsUnifiedRows(installed, catalog);

    expect(rows.map((row) => row.kind)).toEqual([
      "installed",
      "installed",
      "catalog",
      "catalog",
    ]);
    expect(rows[0]).toMatchObject({
      kind: "installed",
      skill: { name: "builtin-one" },
    });
    expect(rows[1]).toMatchObject({
      kind: "installed",
      skill: { name: "codex-only" },
    });
    expect(rows[2]).toMatchObject({
      kind: "catalog",
      skill: { name: "fresh-skill" },
    });
    expect(rows[3]).toMatchObject({
      kind: "catalog",
      skill: { name: "another" },
    });
    expect(
      rows.filter(
        (row) => row.kind === "catalog" && row.skill.name === "codex-only",
      ),
    ).toHaveLength(0);
  });

  test("canonical identities are exact and closed for unmanaged skills", () => {
    const managed = installedSkill(
      "000000000000000000000003",
      "cli-skill",
      ["codex"],
      {
        source: "owner/repo-a",
      },
    );
    const builtin = installedSkill(
      "000000000000000000000004",
      "builtin-two",
      ["codex"],
      {
        manager: "builtin",
        owned: false,
        tracked: false,
        capability: { canManage: false, operations: [], reason: "Builtin." },
      },
    );
    expect(installedSkillCatalogId(managed)).toBe("owner/repo-a/cli-skill");
    expect(installedSkillCatalogId(builtin)).toBeNull();
    expect(catalogSkillId(catalogSkill("x", "owner/repo"))).toBe(
      "owner/repo/x",
    );
  });

  test("an honest empty leaderboard stays empty for each selected view", () => {
    expect(skillsEmptyLeaderboardCopy("all-time")).toEqual({
      title: "No All Time Skills",
      detail: "The upstream leaderboard is currently empty.",
    });
    expect(skillsEmptyLeaderboardCopy("trending").title).toBe(
      "No Trending 24h Skills",
    );
    expect(skillsEmptyLeaderboardCopy("hot").title).toBe("No Hot Skills");
  });

  test("switching Agent synchronously replaces the installed projection and counts across all six", () => {
    const current = inventory([
      installedSkill("000000000000000000000001", "codex-only", ["codex"]),
      installedSkill("000000000000000000000002", "claude-only", [
        "claude-code",
      ]),
      installedSkill("000000000000000000000003", "shared", ["codex", "cursor"]),
      installedSkill("000000000000000000000006", "grok-native", ["grok"]),
    ]);

    expect(skillsAgentCounts(current)).toEqual({
      codex: 2,
      "claude-code": 1,
      cursor: 1,
      grok: 1,
      opencode: 0,
      pi: 0,
    });
    expect(
      skillsAgentProjection(current, "codex").skills.map((skill) => skill.name),
    ).toEqual(["codex-only", "shared"]);
    expect(
      skillsAgentProjection(current, "grok").skills.map((skill) => skill.name),
    ).toEqual(["grok-native"]);
  });

  test("install always targets only the selected managed Agent", () => {
    expect(skillsInstallTargets("codex")).toEqual(["codex"]);
    expect(skillsInstallTargets("claude-code")).toEqual(["claude-code"]);
    expect(skillsInstallTargets("cursor")).toEqual(["cursor"]);
    expect(skillsInstallTargets("grok")).toEqual(["grok"]);
    expect(skillsInstallTargets("opencode")).toEqual(["opencode"]);
    expect(skillsInstallTargets("pi")).toEqual(["pi"]);
  });

  test("status, scope, and ownership are stable list facets", () => {
    const owned = installedSkill("000000000000000000000010", "owned", [
      "codex",
    ]);
    const external = installedSkill(
      "000000000000000000000011",
      "external",
      ["codex"],
      {
        manager: "external",
        owned: false,
        enabled: false,
        scope: "project",
      },
    );
    const catalog = [catalogSkill("available", "owner/repo")];
    expect(
      filterSkillsByFacets([owned, external], catalog, {
        status: "disabled",
        scope: "project",
        ownership: "external",
      }),
    ).toEqual({ installed: [external], catalog: [] });
    expect(
      filterSkillsByFacets([owned, external], catalog, {
        status: "available",
        scope: "all",
        ownership: "catalog",
      }),
    ).toEqual({ installed: [], catalog });
  });

  test("an adopted unbound package remains visible for explicit binding", () => {
    const adopted = installedSkill("000000000000000000000012", "adopted", [], {
      bindings: [],
      agents: [],
      scope: "unknown",
    });
    expect(skillsSectionProjection(inventory([adopted]), "pi").skills).toEqual([
      adopted,
    ]);
  });

  test("ranked and plain catalog rows share one canonical identity", () => {
    const ranked: RankedCatalogSkill = {
      id: "owner/repo/ranked",
      skillId: "ranked",
      name: "ranked",
      source: "owner/repo",
      rank: 1,
      totalInstalls: 10,
      installable: true,
    };
    expect(catalogSkillId(ranked)).toBe("owner/repo/ranked");
    expect(catalogSkillId(catalogSkill("plain", "owner/repo"))).toBe(
      "owner/repo/plain",
    );
  });

  test("shared mobile input geometry keeps one centered text lane at normal and enlarged font scales", () => {
    expect(MOBILE_SINGLE_LINE_INPUT_LAYOUT.controlHeight).toBe(48);
    expect(
      mobileSingleLineTextLaneWidth(288, true, true),
    ).toBeGreaterThanOrEqual(
      MOBILE_SINGLE_LINE_INPUT_LAYOUT.minimumTextLaneWidth,
    );
    expect(
      mobileSingleLineTextLaneWidth(343, true, true),
    ).toBeGreaterThanOrEqual(
      MOBILE_SINGLE_LINE_INPUT_LAYOUT.minimumTextLaneWidth,
    );
    expect(mobileSingleLineScaledLineHeight(1)).toBeLessThan(
      MOBILE_SINGLE_LINE_INPUT_LAYOUT.controlHeight,
    );
    expect(mobileSingleLineScaledLineHeight(1.5)).toBeLessThan(
      MOBILE_SINGLE_LINE_INPUT_LAYOUT.controlHeight,
    );
    expect(mobileSingleLineScaledLineHeight(2)).toBe(
      mobileSingleLineScaledLineHeight(1.5),
    );
  });
});
