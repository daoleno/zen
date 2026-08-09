import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const screenSource = readFileSync(join(import.meta.dir, "../app/skills.tsx"), "utf8");
const presentationSource = readFileSync(
  join(import.meta.dir, "../components/skills/SkillsPresentation.tsx"),
  "utf8",
);
const pluginModelSource = readFileSync(
  join(import.meta.dir, "pluginsScreenModel.ts"),
  "utf8",
);
const surfaceModelSource = readFileSync(
  join(import.meta.dir, "pluginsSkillsSurfaceModel.ts"),
  "utf8",
);

describe("Plugins & Skills V3 action wiring", () => {
  test("all inventory and discovery flows stay bound to the current server", () => {
    expect(screenSource).toContain("useCurrentServer");
    expect(screenSource).toContain("requestOwnerRef.current.rebind(currentServerId)");
    expect(screenSource).toContain("presentationIsCurrent");
    expect(screenSource).toContain('key={currentServerId ?? "none"}');
    expect(screenSource).toContain("const visibleDiscover = presentationIsCurrent");
    expect(screenSource).toContain('query: ""');
    expect(screenSource).toContain('submittedQuery: ""');
    expect(presentationSource).not.toContain("serverBindingKey");
  });

  test("inventory, Plugins, and leaderboards refresh through one data-retaining request state", () => {
    expect(screenSource.match(/beginSkillsRequest\(current, token\.generation\)/g)).toHaveLength(3);
    expect(screenSource).toContain("skillsRequestData(visibleInventoryState)");
    expect(screenSource).toContain("skillsRequestData(visiblePluginsState)");
    expect(screenSource).toContain("skillsRequestData(visibleCatalogState)");
    expect(screenSource).toContain("useMemo(\n    () => pluginSectionView");
    expect(presentationSource).toContain(
      "skillsRequestData(state) !== undefined",
    );
  });

  test("new-query search clears data while same-query refresh and retry retain it", () => {
    expect(screenSource).toContain(
      'void runSearch(transition.effect.query, "new-query")',
    );
    expect(screenSource).toContain(
      'void runSearch(discover.submittedQuery, "same-query")',
    );
    expect(screenSource).toContain(
      'intent: "new-query" | "same-query"',
    );
    expect(screenSource).toContain(
      'intent === "same-query"',
    );
  });

  test("refresh and retained-data errors keep each FlatList and its rows mounted", () => {
    const installedPlugins = presentationSource.match(
      /function InstalledPluginsList\([\s\S]*?function InstalledPluginItem\(/,
    )?.[0];
    const installedSkills = presentationSource.match(
      /function InstalledSkillsList\([\s\S]*?function InstalledSkillItem\(/,
    )?.[0];
    const discoverSkills = presentationSource.match(
      /function DiscoverSkillsList\([\s\S]*?function CatalogRow\(/,
    )?.[0];
    expect(installedPlugins).toContain("data={visibleRows}");
    expect(installedPlugins).toContain(
      'loading={state.status === "loading" && !hasData}',
    );
    expect(installedPlugins).toContain(
      'error={state.status === "error" ? state.error : undefined}',
    );
    expect(installedSkills).toContain("data={visibleSkills}");
    expect(installedSkills).toContain(
      'loading={state.status === "loading" && !hasInventory}',
    );
    expect(discoverSkills).toContain("data={data}");
    expect(discoverSkills).toContain(
      'loading={catalogState.status === "loading" && !hasCatalog}',
    );
    expect(discoverSkills).toContain(
      'error={catalogState.status === "error" ? catalogState.error : undefined}',
    );
  });

  test("an older daemon is a concise Plugin capability error, never a cache projection", () => {
    expect(screenSource).toContain('error.code === "unknown_message_type"');
    expect(screenSource).toContain("Update the Zen daemon to manage Plugins.");
    expect(screenSource).not.toContain("projectPlugins");
    expect(screenSource).not.toContain("pluginsFallback");
    expect(presentationSource).not.toContain("FallbackPlugin");
    expect(presentationSource).not.toContain("fallbackPlugins");
    expect(pluginModelSource).not.toContain("CacheFallbackPlugin");
    expect(pluginModelSource).not.toContain("projectPlugins");
    expect(surfaceModelSource).not.toContain("filterFallbackPlugins");
  });

  test("Plugins install, update, and uninstall prepare real daemon commands", () => {
    expect(screenSource).toContain("wsClient.getPluginsInventory");
    expect(screenSource).toContain("wsClient.buildPluginCommand");
    expect(screenSource).toContain('preparePluginMutation("install"');
    expect(screenSource).toContain('preparePluginMutation("update"');
    expect(screenSource).toContain('preparePluginMutation("uninstall"');
    expect(screenSource).toContain("evaluatePluginMutation");
    expect(presentationSource).toContain("onInstallPlugin");
    expect(presentationSource).toContain("onUpdatePlugin");
    expect(presentationSource).toContain("onUninstallPlugin");
  });

  test("Skills install, remove, and scope update retain the npx skills daemon gate", () => {
    expect(screenSource).toContain("wsClient.buildSkillsCommand");
    expect(screenSource).toContain("evaluateSkillMutation");
    expect(screenSource).toContain("skillsInstallTargets(selectedAgent)");
    expect(screenSource).toContain("skillsRemovalPlanForAgent");
    expect(screenSource).toContain("projectUpdateAvailable");
    expect(presentationSource).toContain('label="Global Skills"');
    expect(presentationSource).toContain('label="Project Skills"');
  });

  test("reviewed lifecycle commands still hand off to a real owned Terminal session", () => {
    expect(screenSource).toContain("createOwnedSkillsTerminalSession");
    expect(screenSource).toContain("skillsTerminalHandoff.issue");
    expect(screenSource).toContain("buildSkillsMutationConfirmation");
    expect(screenSource).toContain("buildPluginMutationConfirmation");
    expect(screenSource).toContain('initialInterfaceRenderMode: "terminal"');
  });

  test("unsupported ownership exposes inspection, not fake mutation callbacks", () => {
    const pluginRow = presentationSource.match(
      /function InstalledPluginItem\([\s\S]*?function DiscoverPluginsList\(/,
    )?.[0];
    const skillRow = presentationSource.match(
      /function InstalledSkillItem\([\s\S]*?function DiscoverSkillsList\(/,
    )?.[0];
    expect(pluginRow).toBeDefined();
    expect(pluginRow).toContain("ownership.manageable");
    expect(pluginRow).toContain("information-circle-outline");
    expect(pluginRow?.match(/<Pressable/g)).toHaveLength(1);
    expect(pluginRow).toContain("ItemActionIndicator");
    expect(pluginRow).not.toContain("ItemIconAction");
    expect(pluginRow).not.toContain("onUpdatePlugin");
    expect(pluginRow).not.toContain("onUninstallPlugin");
    expect(skillRow).toBeDefined();
    expect(skillRow).toContain("ownership.manageable");
    expect(skillRow).toContain("information-circle-outline");
  });
});
