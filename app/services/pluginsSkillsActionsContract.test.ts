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
const websocketSource = readFileSync(
  join(import.meta.dir, "websocket.ts"),
  "utf8",
);
const skillsScreenModelSource = readFileSync(
  join(import.meta.dir, "skillsScreenModel.ts"),
  "utf8",
);

describe("Plugins & Skills V4 management surface", () => {
  test("all inventory and discovery flows stay bound to the current server", () => {
    expect(screenSource).toContain("useCurrentServer");
    expect(screenSource).toContain("requestOwnerRef.current.rebind(currentServerId)");
    expect(screenSource).toContain("presentationIsCurrent");
    expect(screenSource).toContain('key={currentServerId ?? "none"}');
    expect(screenSource).toContain("const visibleInventoryState = presentationIsCurrent");
    expect(presentationSource).not.toContain("serverBindingKey");
  });

  test("there is no Installed/Discover mode switch anywhere in the surface", () => {
    // The row discriminator literals are unified-list kinds, not modes; the
    // banned state switch is a top-level Installed/Discover mode selection.
    expect(presentationSource).not.toContain("activeMode");
    expect(presentationSource).not.toContain("onSelectMode");
    expect(presentationSource).not.toContain("PluginsSkillsMode");
    expect(screenSource).not.toContain("SkillsMode");
    expect(screenSource).not.toContain("PluginsMode");
    expect(screenSource).not.toContain("pluginsMode");
    // The only "discover" wording left is the inline search hint, which is
    // item-level discovery copy, not a navigation mode.
    expect(presentationSource).toContain(
      "Use Search above to discover Skills from skills.sh.",
    );
  });

  test("each tab renders one unified management list with installed and discovered rows", () => {
    expect(presentationSource).toContain("skillsUnifiedRows(visibleSkills, visibleCatalog)");
    expect(screenSource).toContain("pluginsUnifiedView");
    expect(presentationSource).toContain("function SkillsList");
    expect(presentationSource).toContain("kind: \"installed\"");
    expect(presentationSource).toContain("function CatalogSkillItem");
    expect(presentationSource).toContain("kind: \"available\"; plugin: AvailablePlugin");
    expect(skillsScreenModelSource).toContain("installedSkillCatalogId");
    expect(pluginModelSource).toContain("pluginsUnifiedView");
  });

  test("lifecycle badges mark source and lifecycle on every row", () => {
    expect(surfaceModelSource).toContain("installedSkillBadges");
    expect(surfaceModelSource).toContain("catalogSkillBadges");
    expect(surfaceModelSource).toContain("installedPluginBadges");
    expect(surfaceModelSource).toContain("availablePluginBadges");
    expect(presentationSource).toContain("function BadgeRow");
  });

  test("refresh retains the rendered list and stable chrome (no mode buttons, no flash)", () => {
    expect(screenSource).toContain("beginSkillsRequest(current, token.generation)");
    expect(screenSource).toContain("skillsRequestData(visibleInventoryState)");
    expect(presentationSource).toContain("skillsRequestData(state) !== undefined");
    // The toolbar renders exactly the stable tool set: Target/Ranking/Update
    // for Skills plus Refresh; no segmented state buttons.
    expect(presentationSource).toContain("function CompactToolbar");
    expect(presentationSource).not.toContain("onOpenMode");
    expect(presentationSource).not.toContain("View Installed");
    expect(presentationSource).not.toContain("View Discover");
  });

  test("the top toolbar is at most one icon-only options control plus refresh", () => {
    const toolbarBlock = presentationSource.match(
      /function CompactToolbar\([\s\S]*?function SurfaceSearch\(/,
    )?.[0]!;
    expect(toolbarBlock).toBeDefined();
    // Exactly one Skills options icon button and one Refresh icon button.
    expect(toolbarBlock.match(/accessibilityLabel="Skills options"/g)).toHaveLength(1);
    expect(toolbarBlock.match(/accessibilityLabel="Refresh"/g)).toHaveLength(1);
    // No labeled top toolbar buttons and no leftover tool-button component.
    expect(toolbarBlock).not.toContain("ToolButton");
    expect(toolbarBlock).not.toContain("skillAgentLabel(selectedAgent)");
    expect(toolbarBlock).not.toMatch(/label=\"Target\"/);
    expect(toolbarBlock).not.toMatch(/label=\"Ranking\"/);
    expect(toolbarBlock).not.toMatch(/label=\"Update\"/);
    expect(toolbarBlock).not.toContain("skillsLeaderboardLabel");
    // The Plugins toolbar has only refresh (the options button is Skills-only).
    expect(presentationSource).toContain('{section === "skills" ? (');
    // Target, Ranking, and Update controls still exist once, inside the sheet.
    expect(presentationSource).toContain("SheetSectionHeading>Target</SheetSectionHeading>");
    expect(presentationSource).toContain("SheetSectionHeading>Ranking</SheetSectionHeading>");
    expect(presentationSource).toContain("SheetSectionHeading>Update</SheetSectionHeading>");
    expect(presentationSource).toContain('kind: "options"');
    expect(presentationSource).not.toContain('kind: "target"');
    expect(presentationSource).not.toContain('kind: "ranking"');
    expect(presentationSource).not.toContain('kind: "skills-update"');
  });

  test("reviewed commands execute through the authoritative daemon mutation path", () => {
    expect(websocketSource).toContain("executeSkillsMutation(");
    expect(websocketSource).toContain('type: "skills_mutation"');
    expect(websocketSource).toContain("skills_mutation_result");
    expect(websocketSource).toContain("executePluginMutation(");
    expect(websocketSource).toContain('type: "plugin_mutation"');
    expect(websocketSource).toContain("plugin_mutation_result");
    // The old terminal handoff execution path is gone.
    expect(screenSource).not.toContain("skillsTerminalHandoff");
    expect(screenSource).not.toContain("handoffToTerminal");
    expect(screenSource).not.toContain("createOwnedSkillsTerminalSession");
  });

  test("destructive actions confirm, then refresh inventory and show truthful state", () => {
    expect(screenSource).toContain("confirmMutation(confirmation, destructive)");
    expect(screenSource).toContain("void refreshInventory()");
    expect(screenSource).toContain("void loadPlugins()");
    expect(screenSource).toContain("mutationNotice");
    expect(screenSource).toContain("kind: \"success\"");
    expect(screenSource).toContain("kind: \"error\"");
    expect(presentationSource).toContain("function MutationNoticeBanner");
  });

  test("unsupported ownership exposes inspection, not fake mutation callbacks", () => {
    expect(presentationSource).toContain("installedSkillOwnership");
    expect(presentationSource).toContain("installedPluginOwnership");
    expect(surfaceModelSource).toContain("manageable: false");
    expect(surfaceModelSource).toContain("Codex-hosted plugins do not expose a supported lifecycle adapter");
    expect(surfaceModelSource).toContain("Discovered from client cache");
  });

  test("the Skills feature uses neutral icons, never sparkle imagery", () => {
    expect(presentationSource).not.toContain("sparkles");
    expect(presentationSource).not.toContain("Sparkles");
    expect(presentationSource).toContain('icon: "library-outline"');
    expect(presentationSource).toContain('icon: "extension-puzzle-outline"');
  });
});