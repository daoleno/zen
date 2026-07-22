import { describe, expect, test } from "bun:test";
import {
  MOBILE_SINGLE_LINE_INPUT_LAYOUT,
  mobileSingleLineScaledLineHeight,
  mobileSingleLineTextLaneWidth,
} from "../components/ui/mobileSingleLineInputModel";
import {
  SkillsAutomaticInventoryOwner,
  createSkillsDiscoverState,
  reduceSkillsDiscover,
  skillsAgentCounts,
  skillsAgentProjection,
  skillsEmptyLeaderboardCopy,
  skillsInstallTargets,
  skillsRemovalPlanForAgent,
} from "./skillsScreenModel";
import type {
  InstalledSkill,
  ManagedSkillAgent,
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
    capability,
  };
}

function inventory(skills: InstalledSkill[]): SkillsInventory {
  return {
    generatedAt: "2026-07-20T00:00:00Z",
    skills,
    agents: [],
    warnings: [],
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

  test("typing is local, submit owns search, and clear restores the selected leaderboard", () => {
    let state = createSkillsDiscoverState();

    let transition = reduceSkillsDiscover(state, {
      type: "select_view",
      view: "trending",
    });
    state = transition.state;
    expect(state.view).toBe("trending");
    expect(transition.effect).toEqual({ type: "none" });

    transition = reduceSkillsDiscover(state, {
      type: "change_query",
      value: "react native",
    });
    state = transition.state;
    expect(state.submittedQuery).toBe("");
    expect(transition.effect).toEqual({ type: "none" });

    transition = reduceSkillsDiscover(state, { type: "submit" });
    state = transition.state;
    expect(transition.effect).toEqual({
      type: "submit_search",
      query: "react native",
    });
    expect(state.submittedQuery).toBe("react native");

    transition = reduceSkillsDiscover(state, { type: "clear" });
    state = transition.state;
    expect(transition.effect).toEqual({ type: "clear_search" });
    expect(state).toEqual({
      query: "",
      submittedQuery: "",
      view: "trending",
    });
  });

  test("one-character submit never searches and an empty edit clears old results", () => {
    let state = createSkillsDiscoverState();
    state = reduceSkillsDiscover(state, {
      type: "change_query",
      value: "r",
    }).state;
    expect(reduceSkillsDiscover(state, { type: "submit" }).effect).toEqual({
      type: "none",
    });

    state = {
      query: "previous",
      submittedQuery: "previous",
      view: "hot",
    };
    const cleared = reduceSkillsDiscover(state, {
      type: "change_query",
      value: "   ",
    });
    expect(cleared.effect).toEqual({ type: "clear_search" });
    expect(cleared.state.view).toBe("hot");
    expect(cleared.state.submittedQuery).toBe("");
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

  test("Discover always targets only the selected managed Agent", () => {
    expect(skillsInstallTargets("codex")).toEqual(["codex"]);
    expect(skillsInstallTargets("claude-code")).toEqual(["claude-code"]);
    expect(skillsInstallTargets("cursor")).toEqual(["cursor"]);
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
