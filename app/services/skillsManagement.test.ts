import { describe, expect, test } from "bun:test";
import {
  assertSkillsMutationMatchesRequest,
  beginSkillsRequest,
  buildSkillsMutationConfirmation,
  completeSkillsRequest,
  createSkillsRequestState,
  failSkillsRequest,
  normalizeSkillsInspectDetail,
  normalizeSkillsInventory,
  normalizeSkillsMutationCommand,
  normalizeSkillsMutationResult,
  skillsRequestData,
  type InstalledSkill,
  type SkillsInventory,
  type SkillsMutationCommand,
} from "./skillsManagement";

describe("skills inventory normalization", () => {
  test("accepts the Zen-owned row contract with per-binding state", () => {
    const value = {
      generated_at: "2026-08-01T00:00:00Z",
      cwd: "/repo",
      skills: [
        {
          id: "abc123def456abc123def456",
          name: "demo",
          description: "A demo Skill",
          manager: "zen",
          owned: true,
          tracked: true,
          enabled: true,
          canonical_path: "/home/u/.zen/skills/store/demo",
          source_path: "/home/u/.zen/skills/store/demo",
          scope: "global",
          agents: ["codex", "cursor"],
          bindings: [
            {
              agent: "codex",
              scope: "global",
              mode: "symlink",
              target_path: "/home/u/.codex/skills/demo",
              source_path: "/home/u/.zen/skills/store/demo",
              enabled: true,
              bound_at: "2026-08-01T00:00:00Z",
            },
            {
              agent: "cursor",
              scope: "global",
              mode: "copy",
              target_path: "/home/u/.cursor/skills/demo",
              source_path: "/home/u/.zen/skills/store/demo",
              enabled: true,
              bound_at: "2026-08-01T00:00:00Z",
              drift_hash: "drifted",
            },
          ],
          provenance: "Zen canonical store",
          source: "owner/repo",
          source_type: "catalog",
          ref: "main",
          content_hash: "abcd",
          installed_at: "2026-08-01T00:00:00Z",
          updated_at: "2026-08-01T00:00:00Z",
          migration: "owned",
          risk: [{ type: "script", severity: "info", detail: "Script file.", file: "run.sh" }],
          capability: {
            can_manage: true,
            operations: [
              "bind",
              "unbind",
              "enable",
              "disable",
              "uninstall",
              "update",
            ],
          },
        },
      ],
      agents: [
        {
          agent: "grok",
          name: "Grok",
          supported: true,
          global_scope: true,
          project_scope: true,
          binding_mode: "copy",
          default_global_dir: "/home/u/.grok/skills",
        },
      ],
      executors: [{ name: "agent", kind: "cursor", agent: "cursor", command: "cursor-agent" }],
      warnings: ["A warning"],
      mutation_operations: [
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
      migration: { owned: 1, external: 2, duplicate: 1, conflict: 0, tracked: 3 },
    };
    const inventory = normalizeSkillsInventory(value);
    expect(inventory.skills).toHaveLength(1);
    const skill = inventory.skills[0]!;
    expect(skill.owned).toBe(true);
    expect(skill.manager).toBe("zen");
    expect(skill.capability.canManage).toBe(true);
    expect(skill.capability.operations).toContain("uninstall");
    expect(skill.bindings[0]!.agent).toBe("codex");
    expect(skill.bindings[1]!.driftHash).toBe("drifted");
    expect(skill.risk?.[0]?.severity).toBe("info");
    expect(inventory.migration.conflict).toBe(0);
    expect(inventory.mutationOperations).toHaveLength(10);
    expect(inventory.executors?.[0]?.agent).toBe("cursor");
  });

  test("rejects duplicate ids, unknown managers, and oversized rows", () => {
    const base = {
      generated_at: "2026-08-01T00:00:00Z",
      skills: [
        {
          id: "abc123def456abc123def456",
          name: "a",
          manager: "zen",
          owned: true,
          tracked: true,
          enabled: true,
          canonical_path: "/s/a",
          source_path: "/s/a",
          scope: "global",
          agents: ["codex"],
          bindings: [
            {
              agent: "codex",
              scope: "global",
              mode: "symlink",
              target_path: "/home/u/.codex/skills/a",
              source_path: "/s/a",
              enabled: true,
              bound_at: "2026-08-01T00:00:00Z",
            },
          ],
          provenance: "store",
          capability: { can_manage: true, operations: ["uninstall"] },
        },
      ],
      agents: [],
      warnings: [],
      mutation_operations: ["import", "uninstall"],
    };
    expect(() => normalizeSkillsInventory(base)).not.toThrow();
    expect(() =>
      normalizeSkillsInventory({
        ...base,
        skills: [base.skills[0], { ...base.skills[0], canonical_path: "/s/b", id: "abc123def456abc123def456" }],
      }),
    ).toThrow();
    expect(() =>
      normalizeSkillsInventory({
        ...base,
        skills: [{ ...base.skills[0], manager: "skills-cli" }],
      }),
    ).toThrow();
    // Missing mutation operations: an older daemon is rejected (fail closed).
    expect(() =>
      normalizeSkillsInventory({
        ...base,
        mutation_operations: undefined,
      }),
    ).toThrow();
  });

  test("external rows are honest about ownership and adopt capability", () => {
    const value = {
      generated_at: "2026-08-01T00:00:00Z",
      skills: [
        {
          id: "111122223333111122223333",
          name: "ext",
          manager: "external",
          owned: false,
          tracked: true,
          enabled: true,
          canonical_path: "/home/u/.grok/skills/ext",
          source_path: "/home/u/.grok/skills/ext",
          scope: "global",
          agents: ["grok"],
          bindings: [
            {
              agent: "grok",
              scope: "global",
              mode: "copy",
              target_path: "/home/u/.grok/skills/ext",
              source_path: "/home/u/.grok/skills/ext",
              enabled: true,
              bound_at: "2026-08-01T00:00:00Z",
            },
          ],
          provenance: "Tracked external installation",
          source: "/home/u/.grok/skills/ext",
          source_type: "external",
          content_hash: "h",
          migration: "external",
          capability: { can_manage: true, operations: ["adopt", "forget"] },
        },
      ],
      agents: [],
      warnings: [],
      mutation_operations: ["adopt", "forget"],
    };
    const inventory = normalizeSkillsInventory(value);
    expect(inventory.skills[0]!.owned).toBe(false);
    expect(inventory.skills[0]!.capability.operations).toEqual([
      "adopt",
      "forget",
    ]);
  });

  test("missing tracked external rows preserve their executable operation and exact reason", () => {
    const reason =
      "external skill directory is unavailable: stat /missing: no such file or directory";
    const inventory = normalizeSkillsInventory({
      generated_at: "2026-08-01T00:00:00Z",
      skills: [
        {
          id: "444455556666444455556666",
          name: "missing-external",
          manager: "external",
          owned: false,
          tracked: true,
          enabled: false,
          canonical_path: "/missing",
          source_path: "/missing",
          scope: "unknown",
          agents: [],
          bindings: [],
          provenance: "Tracked external installation",
          source: "/missing",
          source_type: "external",
          content_hash: "recorded-hash",
          migration: "external",
          capability: {
            can_manage: true,
            operations: ["forget"],
            reason,
          },
        },
      ],
      agents: [],
      warnings: [],
      mutation_operations: ["forget"],
    });

    expect(inventory.skills[0]!.capability).toEqual({
      canManage: true,
      operations: ["forget"],
      reason,
    });
  });
});

describe("mutation command normalization", () => {
  const planBase = {
    operation: "import",
    scope: "global",
    agents: ["codex"],
    skill_name: "demo",
    catalog_id: "owner/repo/demo",
    source: "owner/repo",
    ref: "main",
    summary: "Import demo into Zen's canonical store",
    changes: [
      {
        kind: "create_dir",
        path: "/home/u/.zen/skills/store/demo",
        detail: "Canonical Zen store entry",
      },
      { kind: "symlink", path: "/home/u/.codex/skills/demo", destination: "/home/u/.zen/skills/store/demo" },
    ],
    destructive: false,
  };

  test("accepts the reviewable import plan", () => {
    const command = normalizeSkillsMutationCommand(planBase);
    expect(command.operation).toBe("import");
    expect(command.catalogId).toBe("owner/repo/demo");
    expect(command.ref).toBe("main");
    expect(command.changes).toHaveLength(2);
    expect(command.destructive).toBe(false);
  });

  test("uninstall plans are destructive and exactly described", () => {
    const command = normalizeSkillsMutationCommand({
      operation: "uninstall",
      scope: "global",
      agents: [],
      skill_name: "demo",
      summary: "Uninstall demo (remove all bindings, store content, and inventory entry)",
      changes: [
        { kind: "remove", path: "/home/u/.zen/skills/store/demo", detail: "Remove canonical store content" },
        { kind: "remove", path: "/home/u/.codex/skills/demo", detail: "Remove binding for Codex" },
      ],
      destructive: true,
    });
    expect(command.operation).toBe("uninstall");
    expect(command.destructive).toBe(true);
  });

  test("rejects unbound, unknown, or unreviewable plans", () => {
    expect(() =>
      normalizeSkillsMutationCommand({ ...planBase, catalog_id: undefined, source: undefined }),
    ).toThrow();
    expect(() =>
      normalizeSkillsMutationCommand({ ...planBase, operation: "install" }),
    ).toThrow();
    expect(() =>
      normalizeSkillsMutationCommand({ ...planBase, changes: [] }),
    ).toThrow();
    expect(() =>
      normalizeSkillsMutationCommand({
        ...planBase,
        changes: [{ kind: "delete", path: "/x" }],
      }),
    ).toThrow();
    expect(() =>
      normalizeSkillsMutationCommand({
        ...planBase,
        operation: "migrate",
        skill_name: "demo",
        agents: ["codex"],
      }),
    ).toThrow();
  });

  test("result normalization requires consistent success state", () => {
    const result = normalizeSkillsMutationResult({
      command: planBase,
      success: true,
      exit_code: 0,
      output: "Imported demo (3 files).",
      duration_ms: 12,
    });
    expect(result.execution.success).toBe(true);
    expect(() =>
      normalizeSkillsMutationResult({
        command: planBase,
        success: false,
        exit_code: 0,
        output: "",
        duration_ms: 1,
      }),
    ).toThrow();
  });

  test("executed plans must match the reviewed request exactly", () => {
    const command = normalizeSkillsMutationCommand(planBase);
    assertSkillsMutationMatchesRequest(
      { command, execution: { success: true, exitCode: 0, output: "", durationMs: 1 } },
      {
        operation: "import",
        skillId: "owner/repo/demo",
        source: "owner/repo",
        skillName: "demo",
        scope: "global",
        agents: ["codex"],
      },
    );
    expect(() =>
      assertSkillsMutationMatchesRequest(
        { command, execution: { success: true, exitCode: 0, output: "", durationMs: 1 } },
        {
          operation: "import",
          skillId: "owner/repo/demo",
          source: "owner/other",
          scope: "global",
          agents: ["codex"],
        },
      ),
    ).toThrow();
  });
});

describe("inspect detail normalization", () => {
  test("renders content, provenance, files, bindings, and risk", () => {
    const detail = normalizeSkillsInspectDetail({
      skill_name: "demo",
      description: "A demo",
      manager: "zen",
      owned: true,
      tracked: true,
      enabled: true,
      canonical_path: "/home/u/.zen/skills/store/demo",
      source: "owner/repo",
      source_type: "catalog",
      ref: "main",
      content_hash: "abcd",
      installed_at: "2026-08-01T00:00:00Z",
      updated_at: "2026-08-01T00:00:00Z",
      scope: "global",
      agents: ["codex"],
      bindings: [
        {
          agent: "codex",
          scope: "global",
          mode: "symlink",
          target_path: "/home/u/.codex/skills/demo",
          source_path: "/home/u/.zen/skills/store/demo",
          enabled: true,
          bound_at: "2026-08-01T00:00:00Z",
        },
      ],
      files: [{ path: "SKILL.md", size: 120, mode: "0644" }],
      skill_md: "# Demo\n\nBody with\nnewlines and tabs\there.\n",
      risk: [{ type: "script", severity: "warn", detail: "Script.", file: "run.sh" }],
      warnings: ["Store content drifted."],
      capability: { can_manage: true, operations: ["uninstall", "update"] },
    });
    expect(detail.skillMd).toContain("# Demo");
    expect(detail.skillMd).toContain("\n"); // multiline bodies are preserved
    expect(detail.files?.[0]?.path).toBe("SKILL.md");
    expect(detail.bindings[0]!.enabled).toBe(true);
    expect(detail.risk?.[0]?.severity).toBe("warn");
    expect(detail.capability.operations).toContain("uninstall");
  });

  test("rejects malformed detail payloads", () => {
    expect(() => normalizeSkillsInspectDetail({})).toThrow();
    expect(() =>
      normalizeSkillsInspectDetail({
        skill_name: "demo",
        manager: "zen",
        scope: "bogus",
        agents: ["codex"],
        bindings: [],
      }),
    ).toThrow();
    expect(() =>
      normalizeSkillsInspectDetail({
        skill_name: "demo",
        manager: "zen",
        scope: "global",
        agents: ["codex"],
        bindings: [
          {
            agent: "pi",
            scope: "global",
            mode: "symlink",
            target_path: "/x",
            source_path: "/y",
            enabled: true,
            bound_at: "2026-08-01T00:00:00Z",
          },
        ],
      }),
    ).toThrow();
  });
});

describe("confirmation builder describes exact effects", () => {
  test("destructive uninstall enumerates every removed path", () => {
    const command: SkillsMutationCommand = {
      operation: "uninstall",
      scope: "global",
      agents: ["codex"],
      skillName: "demo",
      summary: "Uninstall demo (remove all bindings, store content, and inventory entry)",
      changes: [
        { kind: "remove", path: "/store/demo", detail: "Remove canonical store content" },
        { kind: "remove", path: "/home/.codex/skills/demo", detail: "Remove binding for Codex" },
      ],
      destructive: true,
    };
    const confirmation = buildSkillsMutationConfirmation(command);
    expect(confirmation.title).toBe("Uninstall demo?");
    expect(confirmation.confirmLabel).toBe("Uninstall");
    expect(confirmation.message).toContain("/store/demo");
    expect(confirmation.message).toContain("/home/.codex/skills/demo");
  });

  test("import confirmation pins provenance", () => {
    const confirmation = buildSkillsMutationConfirmation({
      operation: "import",
      scope: "global",
      agents: ["codex"],
      skillName: "demo",
      catalogId: "owner/repo/demo",
      source: "owner/repo",
      ref: "main",
      summary: "Import demo into Zen's canonical store from owner/repo",
      changes: [{ kind: "create_dir", path: "/store/demo" }],
      destructive: false,
    });
    expect(confirmation.message).toContain("Pinned ref: main");
    expect(confirmation.message).toContain("owner/repo");
  });
});

describe("request state machine", () => {
  test("generations gate begin/complete/fail exactly once", () => {
    const initial = createSkillsRequestState<SkillsInventory>();
    const loading = beginSkillsRequest(initial);
    expect(loading.status).toBe("loading");
    const ready = completeSkillsRequest(loading, loading.generation, {
      generatedAt: "2026-08-01T00:00:00Z",
      skills: [],
      agents: [],
      warnings: [],
      mutationOperations: ["import"],
      migration: { owned: 0, external: 0, duplicate: 0, conflict: 0, tracked: 0 },
    } as SkillsInventory, true);
    expect(ready.status).toBe("empty");
    // A stale completion cannot overwrite a newer generation.
    const newer = beginSkillsRequest(ready);
    expect(completeSkillsRequest(newer, loading.generation, {} as SkillsInventory, false)).toBe(
      newer,
    );
    const failed = failSkillsRequest(newer, newer.generation, "boom");
    expect(failed.status).toBe("error");
    expect(skillsRequestData(failed)).toBeDefined();
  });
});
