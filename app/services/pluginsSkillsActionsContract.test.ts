import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const screenSource = readFileSync(join(import.meta.dir, "../app/skills.tsx"), "utf8");
const presentationSource = readFileSync(
  join(import.meta.dir, "../components/skills/SkillsPresentation.tsx"),
  "utf8",
);

describe("Plugins & Skills V3 action wiring", () => {
  test("all inventory and discovery flows stay bound to the current server", () => {
    expect(screenSource).toContain("useCurrentServer");
    expect(screenSource).toContain("requestOwnerRef.current.rebind(currentServerId)");
    expect(screenSource).toContain("presentationIsCurrent");
    expect(screenSource).toContain('serverBindingKey={currentServerId ?? ""}');
    expect(presentationSource).toContain("serverBindingKey");
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
    expect(pluginRow).not.toContain("onUpdatePlugin");
    expect(pluginRow).not.toContain("onUninstallPlugin");
    expect(skillRow).toBeDefined();
    expect(skillRow).toContain("ownership.manageable");
    expect(skillRow).toContain("information-circle-outline");
  });
});
