import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const drawerSource = readFileSync(
  join(import.meta.dir, "PrimaryDrawerPanel.tsx"),
  "utf8",
);
const skillsSource = readFileSync(
  join(import.meta.dir, "../skills/SkillsPresentation.tsx"),
  "utf8",
);
const settingsSource = readFileSync(
  join(import.meta.dir, "../../app/settings.tsx"),
  "utf8",
);

function drawerRowContractBlock(source: string): string {
  const propsStart = source.indexOf("interface DrawerRowProps");
  const propsEnd = source.indexOf("}", propsStart);
  const fnStart = source.indexOf("function DrawerRow(");
  const fnEnd = source.indexOf("\nexport function PrimaryDrawerPanel", fnStart);
  if (propsStart < 0 || propsEnd < 0 || fnStart < 0 || fnEnd < 0) {
    throw new Error("DrawerRow contract block not found");
  }
  return `${source.slice(propsStart, propsEnd + 1)}\n${source.slice(fnStart, fnEnd)}`;
}

describe("navigation and Skills copy density", () => {
  test("DrawerRow contract cannot express a secondary menu description", () => {
    const contract = drawerRowContractBlock(drawerSource);
    expect(contract).toContain("interface DrawerRowProps");
    expect(contract).toContain("label: string");
    expect(contract).toContain("accessibilityLabel={label}");
    expect(contract).not.toMatch(/\bdetail\b/);
    expect(contract).not.toContain("drawerRowDetail");
    expect(contract).not.toContain("drawerRowCopy");
    expect(drawerSource).not.toContain("drawerRowDetail");
    expect(drawerSource).not.toContain("drawerRowCopy");
    expect(drawerSource).not.toMatch(/detail\?:/);
    expect(drawerSource).not.toMatch(/detail=/);
  });

  test("drawer menu rows keep icon and primary label without secondary descriptions", () => {
    expect(drawerSource).toContain('label="Skills"');
    expect(drawerSource).toContain('label="Stats"');
    expect(drawerSource).toContain('label="Settings"');
    expect(drawerSource).not.toContain("Installed and discover");
    expect(drawerSource).not.toContain("Servers and connection");
    expect(drawerSource).not.toContain("Agent control plane");
  });

  test("drawer connection strip retains short connection status only", () => {
    expect(drawerSource).toContain('"Connected"');
    expect(drawerSource).toContain('"Offline"');
    expect(drawerSource).toContain("currentIssue?.title");
    expect(drawerSource).toContain("connectionSummary");
  });

  test("Settings does not duplicate Connected with a Current badge or header", () => {
    expect(settingsSource).not.toContain("Current:");
    expect(settingsSource).not.toContain("currentServerLabel");
    expect(settingsSource).not.toMatch(/>\s*Current\s*</);
    expect(settingsSource).toContain("connectionLabel(connectionState)");
    expect(settingsSource).toContain('? "Use"');
  });

  test("Skills chrome does not restate selected Agent or invent host-snapshot headers", () => {
    expect(skillsSource).not.toContain("installed for");
    expect(skillsSource).not.toContain("Install targets");
    expect(skillsSource).not.toContain("Host snapshot");
    expect(skillsSource).toContain("AgentKindIcon");
    expect(skillsSource).toContain("shared with");
  });

  test("Plugins and Skills are separate first-level tabs with compact status", () => {
    expect(skillsSource).toContain('{ section: "plugins", label: "Plugins"');
    expect(skillsSource).toContain('{ section: "skills", label: "Skills"');
    expect(skillsSource).toContain("SurfaceTabs");
    expect(skillsSource).toContain("StatusBadge");
    expect(skillsSource).toContain('tone="installed"');
  });

  test("Skills v2 removes explanatory prose and fake per-row updates", () => {
    expect(skillsSource).not.toContain("Enter at least 2 characters");
    expect(skillsSource).not.toContain("View only");
    expect(skillsSource).not.toContain("Switch to Discover");
    // Update is a real action: collection-level Skills update and per-plugin
    // lifecycle. A per-row Skill Update must never exist (the CLI updates
    // whole scopes, not single Skills).
    expect(skillsSource).toContain('label="Update global"');
    expect(skillsSource).toContain('label="Update project"');
    expect(skillsSource).toContain('{ text: "Update", onPress: onUpdate }');
    const installedRowBlock = skillsSource.match(
      /function InstalledSkillRow\([\s\S]*?function DiscoverSkillsList\(/,
    )?.[0];
    expect(installedRowBlock).not.toContain("Update");
    expect(installedRowBlock).toContain('label="Remove"');
  });

  test("Skills retains judgment-critical provenance, remove impact, and error reasons", () => {
    expect(skillsSource).toContain("installedSkillProvenance");
    expect(skillsSource).toContain("affectedAgents");
    expect(skillsSource).toContain("state.error");
    expect(skillsSource).toContain("catalogState.error");
    expect(skillsSource).toContain("searchState.error");
    expect(skillsSource).toContain("installedSkillCaption");
  });

  test("the Agent selector never compresses official labels", () => {
    const selectorBlock = skillsSource.match(
      /function AgentSelector\([\s\S]*?function ModeSwitch\(/,
    )?.[0];
    expect(selectorBlock).toBeDefined();
    expect(selectorBlock).toContain("ScrollView");
    expect(selectorBlock).toContain("showsHorizontalScrollIndicator={false}");
    expect(selectorBlock).not.toContain("numberOfLines={1}");
    expect(selectorBlock).not.toContain("flex: 1,");
  });

  test("one Plugin row gets one contextual action control only", () => {
    const rowBlock = skillsSource.match(
      /function InstalledPluginRow\([\s\S]*?function ExplorePluginsList\(/,
    )?.[0];
    expect(rowBlock).toBeDefined();
    expect(rowBlock).toContain("ellipsis-horizontal");
    expect(rowBlock).not.toContain('label="Update"');
    expect(rowBlock).not.toContain("trash-outline");
  });

  test("Plugin badges state catalog proof and cache-only rows explicitly", () => {
    expect(skillsSource).toContain('label={pluginStatusLabel(row)}');
    expect(skillsSource).toContain('"Read-only"');
    expect(skillsSource).toContain('row.source === "catalog"');
  });

  test("every tappable control meets the 44pt minimum touch height", () => {
    for (const key of ["toolbarAction", "modeButton", "leaderboardTab"]) {
      const block = skillsSource.match(
        new RegExp(`${key}: \\{[\\s\\S]*?\\},`),
      )?.[0];
      expect(block, `${key} style block`).toBeDefined();
      expect(block, `${key} minHeight`).toContain("minHeight: 44");
    }
  });
});
