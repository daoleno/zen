import { describe, expect, test } from "bun:test";
import {
  PLUGIN_CACHE_READONLY_REASON,
  PLUGIN_HOST_UNSUPPORTED_REASON,
  evaluatePluginMutation,
  pluginsUnifiedView,
} from "./pluginsScreenModel";
import {
  isPluginID,
  normalizePluginMutationCommand,
  normalizePluginMutationResult,
  assertPluginMutationMatchesRequest,
  normalizePluginsInventory,
  type InstalledPluginRow,
  type PluginInventory,
} from "./pluginsManagement";

function installedRow(
  id: string,
  host: InstalledPluginRow["host"] = "claude",
  mutable = host === "claude",
): InstalledPluginRow {
  const [name, marketplace] = id.split("@");
  return {
    id,
    name: name!,
    marketplace: marketplace!,
    version: "1.0.0",
    scope: "user",
    enabled: true,
    host,
    mutable,
    source: host === "claude" ? "catalog" : "cache",
    skillCount: 1,
    skills: [
      {
        name: "hosted-skill",
        canonicalPath: `/home/test/.claude/plugins/cache/${marketplace}/${name}/1.0.0/skills/hosted-skill`,
        sourcePath: `/home/test/.claude/plugins/cache/${marketplace}/${name}/1.0.0/skills/hosted-skill`,
      },
    ],
  };
}

function pluginInventory(
  catalogStatus: "ready" | "unavailable",
  installed: InstalledPluginRow[] = [],
): PluginInventory {
  return {
    generatedAt: "2026-08-08T00:00:00Z",
    catalog:
      catalogStatus === "ready"
        ? {
            status: "ready",
            available: [
              {
                pluginId: "plug-a@market-a",
                name: "plug-a",
                marketplaceName: "market-a",
                installable: true,
              },
              {
                pluginId: "plug-b@market-b",
                name: "plug-b",
                marketplaceName: "market-b",
                installable: false,
              },
            ],
            installed: [],
          }
        : {
            status: "unavailable",
            available: [],
            installed: [],
            code: "claude_catalog_unavailable",
            message: "claude CLI missing",
          },
    installed,
    warnings: [],
  };
}

describe("Plugins unified view", () => {
  test("installed and available rows coexist deterministically, deduplicated by identity", () => {
    const view = pluginsUnifiedView(
      pluginInventory("ready", [
        installedRow("plug-b@market-b"),
        installedRow("plug-a@market-a"),
      ]),
    );
    expect(view.catalogReady).toBe(true);
    expect(view.rows.map((row) => row.kind)).toEqual([
      "installed",
      "installed",
    ]);
    expect(view.rows[0]).toEqual({
      kind: "installed",
      plugin: expect.objectContaining({ id: "plug-a@market-a" }),
    });
    // plug-a and plug-b are installed, so their catalog twins never render
    // duplicate discovered rows.
    expect(view.rows.every((row) => row.kind === "installed")).toBe(true);
  });

  test("not-yet-installed catalog entries render as discovered rows", () => {
    const view = pluginsUnifiedView(pluginInventory("ready", []));
    expect(view.rows.map((row) => row.kind)).toEqual(["available"]);
    expect(view.rows[0]).toEqual({
      kind: "available",
      plugin: expect.objectContaining({
        pluginId: "plug-a@market-a",
        installable: true,
      }),
    });
    // plug-b is marked not installable by the daemon (installed elsewhere in
    // the owning client); without an installed row it renders as nothing,
    // never as a fake install affordance.
    expect(
      view.rows.some(
        (row) => row.kind === "available" && row.plugin.pluginId === "plug-b@market-b",
      ),
    ).toBe(false);
  });

  test("unavailable catalog keeps installed rows but drops discovered rows", () => {
    const view = pluginsUnifiedView(
      pluginInventory("unavailable", [installedRow("plug-a@market-a")]),
    );
    expect(view.catalogReady).toBe(false);
    expect(view.catalogUnavailableCode).toBe("claude_catalog_unavailable");
    expect(view.rows).toHaveLength(1);
    expect(view.rows[0]).toEqual({ kind: "installed", plugin: expect.objectContaining({ id: "plug-a@market-a" }) });
  });

  test("cache-only installed rows are never duplicated by catalog entries", () => {
    const inventory = pluginInventory("ready", [
      installedRow("plug-c@market-c", "claude"),
    ]);
    inventory.catalog.available = [
      {
        pluginId: "plug-c@market-c",
        name: "plug-c",
        marketplaceName: "market-c",
        installable: false,
      },
    ];
    const view = pluginsUnifiedView(inventory);
    expect(view.rows).toHaveLength(1);
    expect(view.rows[0]).toEqual({ kind: "installed", plugin: expect.objectContaining({ id: "plug-c@market-c" }) });
  });

  test("empty inventory yields an empty authoritative view", () => {
    const view = pluginsUnifiedView(undefined);
    expect(view.rows).toEqual([]);
    expect(view.catalogReady).toBe(false);
  });
});

describe("Plugin mutation gate", () => {
  test("install is supported only for installable catalog entries not already installed", () => {
    const entry = {
      pluginId: "plug-a@market-a",
      name: "plug-a",
      marketplaceName: "market-a",
      installable: true,
    };
    expect(
      evaluatePluginMutation({
        kind: "install",
        entry,
        installedIds: new Set(),
      }),
    ).toEqual({ supported: true, operation: "install" });

    expect(
      evaluatePluginMutation({
        kind: "install",
        entry: { ...entry, installable: false },
        installedIds: new Set(),
      }),
    ).toEqual({
      supported: false,
      reason: "This plugin is already installed on this server.",
    });

    expect(
      evaluatePluginMutation({
        kind: "install",
        entry,
        installedIds: new Set(["plug-a@market-a"]),
      }),
    ).toEqual({
      supported: false,
      reason: "This plugin is already installed on this server.",
    });
  });

  test("update and uninstall require a mutable Claude-hosted row", () => {
    const claude = installedRow("plug-a@market-a");
    expect(evaluatePluginMutation({ kind: "update", row: claude })).toEqual({
      supported: true,
      operation: "update",
    });
    expect(evaluatePluginMutation({ kind: "uninstall", row: claude })).toEqual({
      supported: true,
      operation: "uninstall",
    });

    const codex = installedRow("plug-b@market-b", "codex");
    for (const kind of ["update", "uninstall"] as const) {
      expect(evaluatePluginMutation({ kind, row: codex })).toEqual({
        supported: false,
        reason: PLUGIN_HOST_UNSUPPORTED_REASON,
      });
    }
  });

  test("cache-derived rows are read-only even when a cache marks them mutable", () => {
    // The daemon never emits a mutable cache row; the gate must still fail
    // closed so a cache path can never authorize lifecycle actions.
    const cacheRow = {
      ...installedRow("plug-a@market-a"),
      source: "cache" as const,
      mutable: true,
    };
    for (const kind of ["update", "uninstall"] as const) {
      expect(evaluatePluginMutation({ kind, row: cacheRow })).toEqual({
        supported: false,
        reason: PLUGIN_CACHE_READONLY_REASON,
      });
    }
  });
});

describe("Plugins wire parsing", () => {
  test("normalizes a bounded authoritative inventory", () => {
    const parsed = normalizePluginsInventory({
      generated_at: "2026-08-08T00:00:00Z",
      catalog: {
        status: "ready",
        available: [
          {
            plugin_id: "plug-a@market-a",
            name: "plug-a",
            marketplace_name: "market-a",
            installable: true,
          },
        ],
        installed: [],
      },
      installed: [
        {
          id: "plug-a@market-a",
          name: "plug-a",
          marketplace: "market-a",
          version: "1.0.0",
          scope: "user",
          enabled: true,
          host: "claude",
          mutable: true,
          source: "catalog",
          skill_count: 1,
          skills: [
            {
              name: "hosted",
              canonical_path: "/x/skills/hosted",
              source_path: "/x/skills/hosted",
            },
          ],
        },
      ],
      warnings: [],
    });
    expect(parsed.catalog.status).toBe("ready");
    expect(parsed.installed).toHaveLength(1);
    expect(parsed.installed[0]?.id).toBe("plug-a@market-a");
    expect(parsed.installed[0]?.skills[0]?.name).toBe("hosted");
  });

  test("rejects malformed inventories and mismatched identities", () => {
    expect(() =>
      normalizePluginsInventory({
        generated_at: "2026-08-08T00:00:00Z",
        catalog: { status: "bogus" },
        installed: [],
      }),
    ).toThrow();
    expect(() =>
      normalizePluginsInventory({
        generated_at: "2026-08-08T00:00:00Z",
        catalog: { status: "ready", available: [], installed: [] },
        installed: [
          {
            id: "plug-a@market-a",
            name: "different-name",
            marketplace: "market-a",
            version: "1.0.0",
            scope: "user",
            enabled: true,
            host: "claude",
            mutable: true,
            source: "catalog",
            skill_count: 0,
            skills: [],
          },
        ],
      }),
    ).toThrow();
    expect(() =>
      normalizePluginsInventory({
        generated_at: "2026-08-08T00:00:00Z",
        catalog: { status: "ready", available: [], installed: [] },
        installed: [
          {
            id: "plug-a@market-a",
            name: "plug-a",
            marketplace: "market-a",
            version: "1.0.0",
            scope: "user",
            enabled: true,
            host: "claude",
            mutable: true,
            source: "catalog",
            skill_count: 2,
            skills: [],
          },
        ],
      }),
    ).toThrow();
  });

  test("normalizes exact official plugin commands and rejects others", () => {
    const update = normalizePluginMutationCommand({
      operation: "update",
      command: "claude plugin update plug-a@market-a --scope user",
      plugin_id: "plug-a@market-a",
      scope: "user",
      host: "claude",
    });
    expect(update.operation).toBe("update");

    const uninstall = normalizePluginMutationCommand({
      operation: "uninstall",
      command: "claude plugin uninstall plug-a@market-a --scope user --yes",
      plugin_id: "plug-a@market-a",
      scope: "user",
      host: "claude",
    });
    expect(uninstall.operation).toBe("uninstall");

    const install = normalizePluginMutationCommand({
      operation: "install",
      command: "claude plugin install plug-a@market-a --scope user",
      plugin_id: "plug-a@market-a",
      scope: "user",
      host: "claude",
    });
    expect(install.operation).toBe("install");

    for (const bad of [
      {
        operation: "update",
        command: "claude plugin update plug-a@market-a --scope user",
        plugin_id: "plug-b@market-b",
        scope: "user",
        host: "claude",
      },
      {
        operation: "update",
        command: "claude plugin update plug-a@market-a --scope project",
        plugin_id: "plug-a@market-a",
        scope: "user",
        host: "claude",
      },
      {
        operation: "uninstall",
        command: "claude plugin uninstall plug-a@market-a --scope user",
        plugin_id: "plug-a@market-a",
        scope: "user",
        host: "claude",
      },
      {
        operation: "install",
        command: "claude plugin install plug-a@market-a --scope user --yes",
        plugin_id: "plug-a@market-a",
        scope: "user",
        host: "claude",
      },
      {
        operation: "update",
        command: "claude plugin update plug-a@market-a;rm -rf / --scope user",
        plugin_id: "plug-a@market-a",
        scope: "user",
        host: "claude",
      },
    ]) {
      expect(() => normalizePluginMutationCommand(bad)).toThrow();
    }
  });

  test("plugin identities reject shell and traversal grammars", () => {
    for (const valid of [
      "clangd-lsp@claude-plugins-official",
      "codex@openai-codex",
      "42crunch-api-security-testing@claude-plugins-official",
    ]) {
      expect(isPluginID(valid)).toBe(true);
    }
    for (const invalid of [
      "",
      "../x@market",
      "plug-one@market-a;echo",
      "plug one@market",
      "plug-one@Market-A",
      "plug-one",
      "plug@market@extra",
    ]) {
      expect(isPluginID(invalid)).toBe(false);
    }
    // A plugin id that passes the literal grammar must never carry shell
    // syntax through an installed row.
    expect(() =>
      normalizePluginsInventory({
        generated_at: "2026-08-08T00:00:00Z",
        catalog: { status: "ready", available: [], installed: [] },
        installed: [
          {
            id: "plug-a@market-a;rm -rf /",
            name: "plug-a",
            marketplace: "market-a",
            version: "1.0.0",
            scope: "user",
            enabled: true,
            host: "claude",
            mutable: true,
            source: "catalog",
            skill_count: 0,
            skills: [],
          },
        ],
      }),
    ).toThrow();
  });
});

describe("Plugin mutation execution boundary", () => {
  test("normalizes a truthful execution result and rejects contradictions", () => {
    const success = normalizePluginMutationResult({
      command: {
        operation: "uninstall",
        command: "claude plugin uninstall plug-a@market-a --scope user --yes",
        plugin_id: "plug-a@market-a",
        scope: "user",
        host: "claude",
      },
      success: true,
      exit_code: 0,
      output: "uninstalled",
      duration_ms: 512,
    });
    expect(success.execution).toEqual({
      success: true,
      exitCode: 0,
      output: "uninstalled",
      durationMs: 512,
    });
    expect(() =>
      normalizePluginMutationResult({
        ...success,
        success: true,
        exit_code: 1,
      }),
    ).toThrow();
  });

  test("executed plugin commands must match the reviewed request exactly", () => {
    const result = normalizePluginMutationResult({
      command: {
        operation: "update",
        command: "claude plugin update plug-a@market-a --scope user",
        plugin_id: "plug-a@market-a",
        scope: "user",
        host: "claude",
      },
      success: true,
      exit_code: 0,
      output: "",
      duration_ms: 10,
    });
    expect(() =>
      assertPluginMutationMatchesRequest(result, {
        operation: "update",
        pluginId: "plug-a@market-a",
        scope: "user",
      }),
    ).not.toThrow();
    for (const mismatch of [
      { operation: "uninstall" as const, pluginId: "plug-a@market-a", scope: "user" as const },
      { operation: "update" as const, pluginId: "plug-b@market-b", scope: "user" as const },
    ]) {
      expect(() => assertPluginMutationMatchesRequest(result, mismatch)).toThrow();
    }
  });
});
