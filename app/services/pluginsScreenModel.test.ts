import { describe, expect, test } from "bun:test";
import {
  assertPluginMutationMatchesRequest,
  buildPluginMutationConfirmation,
  normalizePluginMutationCommand,
  normalizePluginMutationResult,
  normalizePluginsInventory,
  pluginUninstallInput,
  type InstalledPluginCopy,
  type PluginInventory,
} from "./pluginsManagement";
import {
  evaluatePluginUninstall,
  filterLogicalPlugins,
  groupLogicalPlugins,
  pluginCopyLabel,
  pluginReadonlyReason,
  pluginRowMetadata,
} from "./pluginsScreenModel";

function copy(
  name: string,
  overrides: Partial<InstalledPluginCopy> = {},
): InstalledPluginCopy {
  const marketplace = overrides.marketplace || "personal";
  const host = overrides.host || "codex";
  const copyId =
    overrides.copyId ||
    `${host}${marketplace}${name}`
      .replace(/[^a-f0-9]/g, "a")
      .padEnd(24, "a")
      .slice(0, 24);
  return {
    copyId,
    pluginId: `${name}@${marketplace}`,
    name,
    displayName: name[0]!.toUpperCase() + name.slice(1),
    description: `${name} description`,
    marketplace,
    version: "1.2.3",
    scope: "user",
    enabled: true,
    host,
    source: "manager",
    rootPath: `/home/test/.${host}/plugins/cache/${marketplace}/${name}/1.2.3`,
    canonicalPath: `/home/test/.${host}/plugins/cache/${marketplace}/${name}/1.2.3`,
    allowedRoot: `/home/test/.${host}/plugins/cache/${marketplace}/${name}`,
    location: `${host === "codex" ? "Codex" : "Claude Code"} user Plugins`,
    revision: "b".repeat(64),
    agents: [host === "codex" ? "codex" : "claude-code"],
    components: [{ kind: "skill", name: "helper", path: "skills/helper" }],
    capability: { canUninstall: true },
    ...overrides,
  };
}

function wireCopy(value: InstalledPluginCopy): Record<string, unknown> {
  return {
    copy_id: value.copyId,
    plugin_id: value.pluginId,
    name: value.name,
    display_name: value.displayName,
    description: value.description,
    marketplace: value.marketplace,
    version: value.version,
    scope: value.scope,
    enabled: value.enabled,
    host: value.host,
    source: value.source,
    root_path: value.rootPath,
    canonical_path: value.canonicalPath,
    allowed_root: value.allowedRoot,
    location: value.location,
    revision: value.revision,
    agents: value.agents,
    components: value.components,
    capability: {
      can_uninstall: value.capability.canUninstall,
      reason: value.capability.reason,
    },
  };
}

describe("Plugin copy screen model", () => {
  test("groups same-name copies without inventing priority", () => {
    const codex = copy("shared", { copyId: "a".repeat(24) });
    const claude = copy("shared", {
      copyId: "b".repeat(24),
      host: "claude",
      marketplace: "official",
      pluginId: "shared@official",
      agents: ["claude-code"],
      version: "2.0.0",
    });
    const [logical] = groupLogicalPlugins([claude, codex]);
    expect(logical?.copies.map((item) => item.copyId)).toEqual([
      claude.copyId,
      codex.copyId,
    ]);
    expect(logical?.agents).toEqual(["claude-code", "codex"]);
    expect(logical?.versions).toEqual(["1.2.3", "2.0.0"]);
    expect(pluginRowMetadata(logical!)).toBe("v1.2.3 · v2.0.0");
  });

  test("filters installed Plugins by Agent, capability, component, and location", () => {
    const managed = groupLogicalPlugins([copy("alpha")])[0]!;
    const readonly = groupLogicalPlugins([
      copy("beta", {
        copyId: "c".repeat(24),
        source: "remote_cache",
        agents: ["codex"],
        location: "Codex remote Plugin cache",
        capability: {
          canUninstall: false,
          reason: "Provided by Codex and cannot be removed here.",
        },
      }),
    ])[0]!;
    expect(
      filterLogicalPlugins([managed, readonly], "helper", {
        agents: [],
        capability: "all",
      }),
    ).toHaveLength(2);
    expect(
      filterLogicalPlugins([managed, readonly], "remote", {
        agents: [],
        capability: "readonly",
      }),
    ).toEqual([readonly]);
    expect(
      filterLogicalPlugins([managed, readonly], "", {
        agents: ["claude-code"],
        capability: "all",
      }),
    ).toEqual([]);
  });

  test("readonly copies expose a reason and no supported uninstall", () => {
    const readonly = copy("plugin-management", {
      source: "remote_cache",
      capability: {
        canUninstall: false,
        reason:
          "Provided by Codex to manage Plugins and cannot be removed here.",
      },
    });
    expect(evaluatePluginUninstall(readonly)).toEqual({
      supported: false,
      reason: readonly.capability.reason!,
    });
    expect(pluginReadonlyReason(readonly)).toContain("cannot be removed");
    expect(pluginCopyLabel(readonly)).toBe("Codex · v1.2.3 · @personal");
  });
});

describe("Plugin wire boundary", () => {
  test("normalizes exact copies and keeps Plugin lifecycle independent", () => {
    const installed = copy("alpha");
    const inventory = normalizePluginsInventory({
      generated_at: "2026-08-18T00:00:00Z",
      installed: [wireCopy(installed)],
      available: [
        {
          plugin_id: "beta@personal",
          name: "beta",
          marketplace_name: "personal",
          version: "0.1.0",
          host: "codex",
          installable: true,
        },
      ],
      warnings: [],
    });
    expect(inventory.installed[0]).toEqual(installed);
    expect(inventory.available[0]?.pluginId).toBe("beta@personal");
    expect(
      (inventory as PluginInventory & { skills?: unknown }).skills,
    ).toBeUndefined();
  });

  test("preserves unknown Agent names for the neutral logo fallback", () => {
    const installed = copy("future", {
      agents: ["future-agent"],
      capability: {
        canUninstall: false,
        reason: "Provided by a future Agent manager.",
      },
    });
    const inventory = normalizePluginsInventory({
      generated_at: "2026-08-18T00:00:00Z",
      installed: [wireCopy(installed)],
      available: [],
      warnings: [],
    });
    const [logical] = groupLogicalPlugins(inventory.installed);
    expect(logical?.agents).toEqual(["future-agent"]);
    expect(
      filterLogicalPlugins([logical!], "future-agent", {
        agents: [],
        capability: "all",
      }),
    ).toEqual([logical!]);
  });

  test("rejects duplicate copy identity and malformed capabilities", () => {
    const installed = copy("alpha");
    expect(() =>
      normalizePluginsInventory({
        generated_at: "2026-08-18T00:00:00Z",
        installed: [wireCopy(installed), wireCopy(installed)],
        available: [],
      }),
    ).toThrow("invalid installed Plugin copy");
    expect(() =>
      normalizePluginsInventory({
        generated_at: "2026-08-18T00:00:00Z",
        installed: [
          { ...wireCopy(installed), capability: { can_uninstall: "yes" } },
        ],
        available: [],
      }),
    ).toThrow("invalid installed Plugin copy");
  });

  test("requires complete uninstall review identity", () => {
    const installed = copy("alpha");
    const input = pluginUninstallInput(installed);
    const command = normalizePluginMutationCommand({
      operation: "uninstall",
      plugin_id: installed.pluginId,
      host: installed.host,
      source: installed.source,
      scope: "user",
      copy_id: installed.copyId,
      name: installed.name,
      display_name: installed.displayName,
      version: installed.version,
      root_path: installed.rootPath,
      canonical_path: installed.canonicalPath,
      allowed_root: installed.allowedRoot,
      location: installed.location,
      revision: installed.revision,
      agents: installed.agents,
      summary: "Permanently uninstall Alpha",
      destructive: true,
    });
    expect(command.copyId).toBe(installed.copyId);
    const confirmation = buildPluginMutationConfirmation(command);
    expect(confirmation.title).toBe("Uninstall Alpha?");
    expect(confirmation.message).toContain("Available to: Codex");
    expect(confirmation.message).toContain(installed.location);
    expect(confirmation.message).toContain("permanently removes this exact");

    const result = normalizePluginMutationResult({
      command: {
        operation: "uninstall",
        plugin_id: installed.pluginId,
        host: installed.host,
        source: installed.source,
        scope: "user",
        copy_id: installed.copyId,
        name: installed.name,
        display_name: installed.displayName,
        version: installed.version,
        root_path: installed.rootPath,
        canonical_path: installed.canonicalPath,
        allowed_root: installed.allowedRoot,
        location: installed.location,
        revision: installed.revision,
        agents: installed.agents,
        summary: "Permanently uninstall Alpha",
        destructive: true,
      },
      success: true,
      exit_code: 0,
      output: "Uninstalled Alpha.",
      duration_ms: 12,
    });
    expect(() =>
      assertPluginMutationMatchesRequest(result, input),
    ).not.toThrow();
    expect(() =>
      assertPluginMutationMatchesRequest(result, {
        ...input,
        revision: "c".repeat(64),
      }),
    ).toThrow("different Plugin copy");
  });

  test("install review remains available without exposing Discovery rows", () => {
    const command = normalizePluginMutationCommand({
      operation: "install",
      plugin_id: "alpha@personal",
      host: "codex",
      scope: "user",
      name: "alpha",
      agents: ["codex"],
      summary: "Install alpha for Codex",
      destructive: false,
    });
    expect(command.operation).toBe("install");
    expect(command.copyId).toBeUndefined();
  });
});
