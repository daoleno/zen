import { describe, expect, test } from "bun:test";
import {
  beginSkillsRequest,
  buildSkillsMutationConfirmation,
  completeSkillsRequest,
  createSkillsRequestState,
  failSkillsRequest,
  isCatalogIdentity,
  normalizeSkillsCatalogResult,
  normalizeSkillsInventory,
  normalizeSkillsLeaderboards,
  normalizeSkillsMutationCommand,
  type SkillsMutationCommand,
} from "./skillsManagement";

describe("Skills management wire boundary", () => {
  test("normalizes a bounded Installed projection and preserves unmanaged rows", () => {
    const inventory = normalizeSkillsInventory({
      generated_at: "2026-07-19T00:00:00Z",
      cwd: "/workspace/project",
      skills: [
        {
          id: "0123456789abcdef01234567",
          name: "shared-skill",
          description: "Shared guidance",
          canonical_path: "/home/test/.agents/skills/shared-skill",
          source_path: "/workspace/project/.claude/skills/shared-skill",
          scope: "project",
          agents: ["codex", "claude-code", "cursor"],
          bindings: [
            {
              source_path: "/workspace/project/.agents/skills/shared-skill",
              scope: "project",
              agents: ["codex", "cursor"],
            },
            {
              source_path: "/workspace/project/.claude/skills/shared-skill",
              scope: "project",
              agents: ["claude-code"],
            },
          ],
          manager: "skills-cli",
          provenance: "official skills-cli lock",
          source: "acme/skills",
          capability: {
            can_remove: true,
            removal_plans: [
              {
                agent: "codex",
                affected_agents: ["codex", "claude-code", "cursor"],
              },
              {
                agent: "claude-code",
                affected_agents: ["claude-code"],
              },
              {
                agent: "cursor",
                affected_agents: ["codex", "claude-code", "cursor"],
              },
            ],
          },
        },
        {
          id: "89abcdef0123456789abcdef",
          name: "provider-skill",
          canonical_path: "/home/test/.codex/skills/.system/provider-skill",
          source_path: "/home/test/.codex/skills/.system/provider-skill",
          scope: "builtin",
          agents: ["codex"],
          bindings: [
            {
              source_path: "/home/test/.codex/skills/.system/provider-skill",
              scope: "builtin",
              agents: ["codex"],
            },
          ],
          manager: "builtin",
          provenance: "Codex builtin",
          capability: { can_remove: true },
        },
      ],
      agents: [
        {
          agent: "grok",
          name: "Grok",
          supported: false,
          cli_managed: false,
          reason: "No official target.",
        },
      ],
      warnings: [],
    });

    expect(inventory.skills[0]?.agents).toEqual([
      "codex",
      "claude-code",
      "cursor",
    ]);
    expect(inventory.skills[0]?.capability).toEqual({
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
      reason: undefined,
    });
    expect(inventory.skills[1]?.capability).toEqual({
      canRemove: false,
      removalPlans: [],
      reason: undefined,
    });
    expect(inventory.agents[0]).toMatchObject({
      agent: "grok",
      supported: false,
      cliManaged: false,
    });
    expect(() =>
      normalizeSkillsInventory({
        generated_at: "2026-07-19T00:00:00Z",
        skills: [{ name: "silently-dropped" }],
        agents: [],
      }),
    ).toThrow("invalid installed Skill");
  });

  test("normalizes Discover results and rejects malformed or ambiguous identities", () => {
    expect(
      normalizeSkillsCatalogResult({
        query: "react native",
        skills: [
          {
            id: "vercel-labs/agent-skills/react-native",
            name: "react-native",
            installs: 42,
            source: "vercel-labs/agent-skills",
          },
        ],
      }).skills,
    ).toEqual([
      {
        id: "vercel-labs/agent-skills/react-native",
        skillId: "react-native",
        name: "react-native",
        installs: 42,
        source: "vercel-labs/agent-skills",
        installable: true,
      },
    ]);
    expect(isCatalogIdentity("acme/skills/good", "acme/skills", "good")).toBe(
      true,
    );
    expect(
      isCatalogIdentity("acme/skills/good", "acme/skills;touch-pwned", "good"),
    ).toBe(false);
    expect(() =>
      normalizeSkillsCatalogResult({
        query: "good",
        skills: [
          {
            id: "acme/skills/good",
            name: "good",
            installs: 1,
            source: "acme/skills",
          },
          {
            id: "acme/skills/good",
            name: "good",
            installs: 2,
            source: "acme/skills",
          },
        ],
      }),
    ).toThrow("invalid catalog identity");
  });

  test("normalizes all three exact ranked metrics and rejects leaderboard shape drift", () => {
    const leaderboards = normalizeSkillsLeaderboards({
      all_time: {
        view: "all-time",
        total_skills: 9000,
        skills: [
          {
            id: "vercel-labs/skills/find-skills",
            skill_id: "find-skills",
            name: "find-skills",
            source: "vercel-labs/skills",
            rank: 1,
            total_installs: 2500000,
            installable: true,
          },
        ],
      },
      trending: {
        view: "trending",
        total_skills: 8000,
        skills: [
          {
            id: "101-skills/skills/ai-video-generation",
            skill_id: "ai-video-generation",
            name: "ai-video-generation",
            source: "101-skills/skills",
            rank: 1,
            installs_24h: 21000,
            installable: true,
          },
        ],
      },
      hot: {
        view: "hot",
        total_skills: 5000,
        skills: [
          {
            id: "open.feishu.cn/lark-doc",
            skill_id: "lark-doc",
            name: "Lark Doc",
            source: "open.feishu.cn",
            rank: 1,
            current_installs: 244,
            yesterday_installs: 140,
            change: 104,
            installable: false,
          },
        ],
      },
    });

    expect(leaderboards.allTime.skills[0]?.totalInstalls).toBe(2500000);
    expect(leaderboards.trending.skills[0]?.installs24h).toBe(21000);
    expect(leaderboards.hot.skills[0]).toMatchObject({
      currentInstalls: 244,
      yesterdayInstalls: 140,
      change: 104,
      installable: false,
    });

    expect(() =>
      normalizeSkillsLeaderboards({
        all_time: { view: "hot", total_skills: 0, skills: [] },
        trending: { view: "trending", total_skills: 0, skills: [] },
        hot: { view: "hot", total_skills: 0, skills: [] },
      }),
    ).toThrow("mismatched");
  });

  test("mixed-scope canonical rows preserve bindings and cannot be removable", () => {
    const inventory = normalizeSkillsInventory({
      generated_at: "2026-07-19T00:00:00Z",
      skills: [
        {
          id: "0123456789abcdef01234567",
          name: "shared-skill",
          canonical_path: "/shared/shared-skill",
          source_path: "/project/.agents/skills/shared-skill",
          scope: "mixed",
          agents: ["codex"],
          bindings: [
            {
              source_path: "/project/.agents/skills/shared-skill",
              scope: "project",
              agents: ["codex"],
            },
            {
              source_path: "/home/.codex/skills/shared-skill",
              scope: "global",
              agents: ["codex"],
            },
          ],
          manager: "skills-cli",
          provenance: "ambiguous installed binding",
          capability: { can_remove: true, reason: "Mixed scopes." },
        },
      ],
      agents: [],
    });
    expect(inventory.skills[0]?.scope).toBe("mixed");
    expect(inventory.skills[0]?.bindings).toHaveLength(2);
    expect(inventory.skills[0]?.capability.canRemove).toBe(false);
    expect(inventory.skills[0]?.capability.removalPlans).toEqual([]);
  });

  test("fails removal closed when Agent plans are missing, incomplete, or expand beyond installed bindings", () => {
    const normalizeCapability = (removalPlans: unknown) =>
      normalizeSkillsInventory({
        generated_at: "2026-07-19T00:00:00Z",
        skills: [
          {
            id: "0123456789abcdef01234567",
            name: "shared-skill",
            canonical_path: "/project/.agents/skills/shared-skill",
            source_path: "/project/.agents/skills/shared-skill",
            scope: "project",
            agents: ["codex", "cursor"],
            bindings: [
              {
                source_path: "/project/.agents/skills/shared-skill",
                scope: "project",
                agents: ["codex", "cursor"],
              },
            ],
            manager: "skills-cli",
            provenance: "official skills-cli lock",
            capability: {
              can_remove: true,
              removal_plans: removalPlans,
            },
          },
        ],
        agents: [],
      }).skills[0]?.capability;

    expect(normalizeCapability(undefined)).toEqual({
      canRemove: false,
      removalPlans: [],
      reason: "No exact Agent removal plan was proven.",
    });
    expect(
      normalizeCapability([
        { agent: "codex", affected_agents: ["codex", "cursor"] },
      ]),
    ).toMatchObject({ canRemove: false, removalPlans: [] });
    expect(
      normalizeCapability([
        {
          agent: "codex",
          affected_agents: ["codex", "claude-code", "cursor"],
        },
        { agent: "cursor", affected_agents: ["codex", "cursor"] },
      ]),
    ).toMatchObject({ canRemove: false, removalPlans: [] });
  });

  test("accepts daemon nil agent slices as honest empty bindings", () => {
    const inventory = normalizeSkillsInventory({
      generated_at: "2026-07-19T00:00:00Z",
      skills: [
        {
          id: "0123456789abcdef01234567",
          name: "shared-skill",
          canonical_path: "/home/test/.agents/skills/shared-skill",
          source_path: "/home/test/.agents/skills/shared-skill",
          scope: "global",
          agents: ["codex"],
          bindings: [
            {
              source_path: "/home/test/.agents/skills/shared-skill",
              scope: "global",
              agents: null,
            },
            {
              source_path: "/home/test/.codex/skills/shared-skill",
              scope: "global",
              agents: ["codex"],
            },
          ],
          manager: "unknown",
          provenance: "ambiguous installed binding",
          capability: { can_remove: false },
        },
      ],
      agents: [],
    });

    expect(inventory.skills[0]?.bindings[0]?.agents).toEqual([]);
    expect(inventory.skills[0]?.agents).toEqual(["codex"]);
    expect(inventory.skills[0]?.capability.canRemove).toBe(false);
  });

  test("loading generations clear old data and stale completion or failure cannot replace current state", () => {
    let state = createSkillsRequestState<string[]>();
    state = beginSkillsRequest(state);
    state = completeSkillsRequest(state, 1, ["old"], false);
    expect(state).toEqual({ status: "ready", generation: 1, data: ["old"] });

    state = beginSkillsRequest(state);
    expect(state).toEqual({ status: "loading", generation: 2 });
    const current = state;
    expect(completeSkillsRequest(state, 1, ["stale"], false)).toBe(current);
    expect(failSkillsRequest(state, 1, "stale failure")).toBe(current);

    state = failSkillsRequest(state, 2, "current failure");
    expect(state).toEqual({
      status: "error",
      generation: 2,
      error: "current failure",
    });
    expect("data" in state).toBe(false);
  });
});

describe("Skills mutation review", () => {
  const remove: SkillsMutationCommand = {
    operation: "remove",
    command:
      "npx skills remove shared-skill --global --agent codex --agent cursor --yes",
    skillName: "shared-skill",
    scope: "global",
    agents: ["codex", "cursor"],
  };

  const install: SkillsMutationCommand = {
    operation: "install",
    command:
      "npx skills add https://github.com/acme/skills --skill useful --global --agent codex --yes",
    catalogId: "acme/skills/useful",
    source: "acme/skills",
    skillName: "useful",
    scope: "global",
    agents: ["codex"],
  };

  test("accepts only the exact official structured command grammar", () => {
    expect(
      normalizeSkillsMutationCommand({
        operation: remove.operation,
        command: remove.command,
        skill_name: remove.skillName,
        scope: remove.scope,
        agents: remove.agents,
      }),
    ).toEqual(remove);
    expect(
      normalizeSkillsMutationCommand({
        operation: install.operation,
        command: install.command,
        catalog_id: install.catalogId,
        source: install.source,
        skill_name: install.skillName,
        scope: install.scope,
        agents: install.agents,
      }),
    ).toEqual(install);
    expect(() =>
      normalizeSkillsMutationCommand({
        operation: "remove",
        command:
          "npx skills remove shared-skill --global --agent codex --yes; touch pwned",
        skill_name: "shared-skill",
        scope: "global",
        agents: ["codex"],
      }),
    ).toThrow("non-official Skills command");
  });

  test("install commands are bound to the exact catalog repository and current wire version", () => {
    expect(() =>
      normalizeSkillsMutationCommand({
        operation: install.operation,
        command: install.command.replace("acme/skills", "other/skills"),
        catalog_id: install.catalogId,
        source: install.source,
        skill_name: install.skillName,
        scope: install.scope,
        agents: install.agents,
      }),
    ).toThrow("non-official Skills command");
    expect(() =>
      normalizeSkillsMutationCommand({
        operation: install.operation,
        command: install.command,
        skill_name: install.skillName,
        scope: install.scope,
        agents: install.agents,
      }),
    ).toThrow("unbound Skills install command");
  });

  test("removal confirmation names the exact Skill, scope, targets, and command", () => {
    expect(buildSkillsMutationConfirmation(remove)).toEqual({
      title: "Remove shared-skill?",
      message: [
        "Skill: shared-skill",
        "Scope: Global",
        "Affected Agents: Codex, Cursor",
        "",
        "Command:",
        remove.command,
      ].join("\n"),
      confirmLabel: "Remove",
    });
  });
});
