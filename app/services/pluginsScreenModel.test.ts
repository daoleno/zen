import { describe, expect, test } from "bun:test";
import {
  MAX_EXPANDED_PLUGINS,
  PLUGIN_CACHE_READONLY_REASON,
  PLUGIN_HOST_UNSUPPORTED_REASON,
  createPluginExpansionState,
  evaluatePluginMutation,
  pluginSectionView,
  reducePluginExpansion,
} from "./pluginsScreenModel";
import {
  isPluginID,
  normalizePluginMutationCommand,
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

describe("Plugins section view", () => {
  test("authoritative view sorts Installed and Explore deterministically", () => {
    const view = pluginSectionView(
      pluginInventory("ready", [
        installedRow("plug-b@market-b"),
        installedRow("plug-a@market-a"),
      ]),
    );
    expect(view.catalogReady).toBe(true);
    expect(view.installed.map((row) => row.id)).toEqual([
      "plug-a@market-a",
      "plug-b@market-b",
    ]);
    expect(view.explore.map((entry) => entry.pluginId)).toEqual([
      "plug-a@market-a",
      "plug-b@market-b",
    ]);
  });

  test("unavailable catalog keeps Installed rows but drops Explore", () => {
    const view = pluginSectionView(
      pluginInventory("unavailable", [installedRow("plug-a@market-a")]),
    );
    expect(view.catalogReady).toBe(false);
    expect(view.catalogUnavailableCode).toBe("claude_catalog_unavailable");
    expect(view.installed).toHaveLength(1);
    expect(view.explore).toEqual([]);
  });

  test("empty inventory yields an empty authoritative view", () => {
    const view = pluginSectionView(undefined);
    expect(view.installed).toEqual([]);
    expect(view.explore).toEqual([]);
    expect(view.catalogReady).toBe(false);
  });
});

describe("Plugins expansion state", () => {
  test("toggle expands, collapses, and resets deterministically", () => {
    let state = createPluginExpansionState();
    state = reducePluginExpansion(state, { type: "toggle", pluginId: "p1" });
    expect(state.expanded).toEqual(["p1"]);
    state = reducePluginExpansion(state, { type: "toggle", pluginId: "p2" });
    expect(state.expanded).toEqual(["p1", "p2"]);
    state = reducePluginExpansion(state, { type: "toggle", pluginId: "p1" });
    expect(state.expanded).toEqual(["p2"]);
    state = reducePluginExpansion(state, { type: "reset" });
    expect(state.expanded).toEqual([]);
  });

  test("expansion is bounded and evicts the oldest entry first", () => {
    let state = createPluginExpansionState();
    for (let index = 0; index < MAX_EXPANDED_PLUGINS + 3; index += 1) {
      state = reducePluginExpansion(state, {
        type: "toggle",
        pluginId: `p${index}`,
      });
    }
    expect(state.expanded).toHaveLength(MAX_EXPANDED_PLUGINS);
    expect(state.expanded[0]).toBe("p3");
    expect(state.expanded[MAX_EXPANDED_PLUGINS - 1]).toBe(`p${MAX_EXPANDED_PLUGINS + 2}`);
  });

  test("ignores empty plugin identities", () => {
    const state = reducePluginExpansion(createPluginExpansionState(), {
      type: "toggle",
      pluginId: "",
    });
    expect(state.expanded).toEqual([]);
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
