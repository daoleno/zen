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
  skillsEmptyLeaderboardCopy,
  skillsInstallTargets,
  skillsRemovalPlanForAgent,
  skillsUnifiedRows,
} from "./skillsScreenModel";
import type {
  CatalogSkill,
  InstalledSkill,
  ManagedSkillAgent,
  RankedCatalogSkill,
  SkillManagementCapability,
  SkillsInventory,
} from "./skillsManagement";

function installedSkill(
  id: string,
  name: string,
  agents: ManagedSkillAgent[],
  capability: SkillManagementCapability = {
    canRemove: false,
    removalPlans: [],
    reason: "Not CLI managed.",
  },
  source?: string,
): InstalledSkill {
  return {
    id,
    name,
    canonicalPath: `/skills/${name}`,
    sourcePath: `/skills/${name}`,
    scope: "global",
    agents,
    bindings: [{ sourcePath: `/skills/${name}`, scope: "global", agents }],
    manager: capability.canRemove ? "skills-cli" : "unknown",
    provenance: capability.canRemove ? "official skills-cli lock" : "unknown",
    source,
    capability,
  };
}

function inventory(skills: InstalledSkill[]): SkillsInventory {
  return {
    generatedAt: "2026-07-20T00:00:00Z",
    skills,
    agents: [],
    warnings: [],
    mutationOperations: ["install", "remove", "update"],
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
  test("automatic inventory is exactly once per focus and canonical server identity", () => {
    const owner = new SkillsAutomaticInventoryOwner();

    expect(owner.shouldRefresh(0, "server-a")).toBe(false);
    expect(owner.shouldRefresh(1, "server-a")).toBe(true);
    expect(owner.shouldRefresh(1, "server-a")).toBe(false);

    // Agent list, updated-at, CWD, and server object churn never enter this
    // identity. Re-observing the same primitives therefore cannot refresh.
    for (let index = 0; index < 20; index += 1) {
      expect(owner.shouldRefresh(1, "server-a")).toBe(false);
    }

    expect(owner.shouldRefresh(1, "server-b")).toBe(true);
    expect(owner.shouldRefresh(1, "server-b")).toBe(false);
    expect(owner.shouldRefresh(2, "server-b")).toBe(true);
  });

  test("manual inventory refreshes remain repeatable outside the automatic gate", () => {
    const owner = new SkillsAutomaticInventoryOwner();
    let requests = 0;
    const manualRefresh = () => {
      requests += 1;
    };

    if (owner.shouldRefresh(1, "server-a")) requests += 1;
    manualRefresh();
    manualRefresh();

    expect(requests).toBe(3);
    expect(owner.shouldRefresh(1, "server-a")).toBe(false);
  });

  test("unified rows keep installed and discovered together, deduplicated by canonical identity", () => {
    const installed = [
      installedSkill(
        "000000000000000000000001",
        "codex-only",
        ["codex"],
        { canRemove: true, removalPlans: [] },
        "owner/repo-a",
      ),
      installedSkill("000000000000000000000002", "builtin-one", ["codex"]),
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
    // Installed rows are ordered by name; catalog rows keep browse order and
    // drop both duplicates of the installed identity.
    expect(rows[0]).toMatchObject({ kind: "installed", skill: { name: "builtin-one" } });
    expect(rows[1]).toMatchObject({ kind: "installed", skill: { name: "codex-only" } });
    expect(rows[2]).toMatchObject({ kind: "catalog", skill: { name: "fresh-skill" } });
    expect(rows[3]).toMatchObject({ kind: "catalog", skill: { name: "another" } });
    // The catalog twin of an installed Skill never renders twice.
    expect(rows.filter((row) => row.kind === "catalog" && row.skill.name === "codex-only")).toHaveLength(0);
  });

  test("canonical identities are exact and closed for unmanaged skills", () => {
    const managed = installedSkill(
      "000000000000000000000003",
      "cli-skill",
      ["codex"],
      { canRemove: true, removalPlans: [] },
      "owner/repo-a",
    );
    const builtin = installedSkill("000000000000000000000004", "builtin-two", ["codex"]);
    expect(installedSkillCatalogId(managed)).toBe("owner/repo-a/cli-skill");
    expect(installedSkillCatalogId(builtin)).toBeNull();
    expect(catalogSkillId(catalogSkill("x", "owner/repo"))).toBe("owner/repo/x");
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

  test("switching Agent synchronously replaces the installed projection and counts", () => {
    const current = inventory([
      installedSkill("000000000000000000000001", "codex-only", ["codex"]),
      installedSkill("000000000000000000000002", "claude-only", [
        "claude-code",
      ]),
      installedSkill("000000000000000000000003", "shared", ["codex", "cursor"]),
    ]);

    expect(skillsAgentCounts(current)).toEqual({
      codex: 2,
      "claude-code": 1,
      cursor: 1,
      opencode: 0,
      pi: 0,
    });
    expect(
      skillsAgentProjection(current, "codex").skills.map((skill) => skill.name),
    ).toEqual(["codex-only", "shared"]);
    expect(
      skillsAgentProjection(current, "claude-code").skills.map(
        (skill) => skill.name,
      ),
    ).toEqual(["claude-only"]);
    expect(
      skillsAgentProjection(current, "cursor").skills.map(
        (skill) => skill.name,
      ),
    ).toEqual(["shared"]);
  });

  test("install always targets only the selected managed Agent", () => {
    expect(skillsInstallTargets("codex")).toEqual(["codex"]);
    expect(skillsInstallTargets("claude-code")).toEqual(["claude-code"]);
    expect(skillsInstallTargets("cursor")).toEqual(["cursor"]);
    expect(skillsInstallTargets("opencode")).toEqual(["opencode"]);
    expect(skillsInstallTargets("pi")).toEqual(["pi"]);
  });

  test("removal uses only the selected Agent's daemon-proven affected set", () => {
    const shared = installedSkill(
      "000000000000000000000004",
      "shared",
      ["codex", "claude-code", "cursor"],
      {
        canRemove: true,
        removalPlans: [
          {
            agent: "codex",
            affectedAgents: ["codex", "claude-code", "cursor"],
          },
          { agent: "claude-code", affectedAgents: ["claude-code"] },
          {
            agent: "cursor",
            affectedAgents: ["codex", "claude-code", "cursor"],
          },
        ],
      },
    );
    const unsafe = installedSkill("000000000000000000000005", "builtin", [
      "codex",
    ]);

    expect(skillsRemovalPlanForAgent(shared, "codex")?.affectedAgents).toEqual([
      "codex",
      "claude-code",
      "cursor",
    ]);
    expect(
      skillsRemovalPlanForAgent(shared, "claude-code")?.affectedAgents,
    ).toEqual(["claude-code"]);
    expect(skillsRemovalPlanForAgent(unsafe, "codex")).toBeNull();
    expect(skillsRemovalPlanForAgent(shared, "cursor")?.agent).toBe("cursor");
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
    expect(catalogSkillId(catalogSkill("plain", "owner/repo"))).toBe("owner/repo/plain");
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