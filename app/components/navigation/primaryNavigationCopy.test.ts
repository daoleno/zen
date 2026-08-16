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

  test("Skills chrome stays compact and does not restate implementation inventory", () => {
    expect(skillsSource).not.toContain("installed for");
    expect(skillsSource).not.toContain("Install targets");
    expect(skillsSource).not.toContain("Host snapshot");
    expect(skillsSource).toContain("AgentKindIcon");
    expect(skillsSource).toContain("CompactToolbar");
    expect(skillsSource).toContain("SurfaceSearch");
  });

  test("Plugins and Skills have one first-level tablist and no second segmented navigator", () => {
    expect(skillsSource).toContain('section: "plugins"');
    expect(skillsSource).toContain('label: "Plugins"');
    expect(skillsSource).toContain('{ section: "skills", label: "Skills"');
    expect(skillsSource).toContain("SurfaceTabs");
    expect(skillsSource.match(/accessibilityRole="tablist"/g)).toHaveLength(1);
    expect(skillsSource).toContain("accessibilityLabel={tab.label}");
    expect(skillsSource).not.toContain("ModeSwitch");
    expect(skillsSource).not.toContain("PluginsModeSwitch");
  });

  test("one stable surface with real lifecycle actions and no Installed/Discover switch", () => {
    expect(skillsSource).not.toContain("Enter at least 2 characters");
    expect(skillsSource).not.toContain("View only");
    expect(skillsSource).not.toContain("Switch to Discover");
    expect(skillsSource).not.toContain('label="Discover"');
    expect(skillsSource).not.toContain('label="Installed"');
    // Per-row lifecycle actions: owned rows update, external rows adopt or
    // forget; destructive uninstall lives in the inspector with exact paths.
    expect(skillsSource).toContain('label="Update"');
    expect(skillsSource).toContain('label="Adopt"');
    expect(skillsSource).toContain('label="Forget"');
    expect(skillsSource).toContain('label="Uninstall"');
    const installedRowBlock = skillsSource.match(
      /function InstalledSkillRow\([\s\S]*?function CatalogSkillRow\(/,
    )?.[0];
    expect(installedRowBlock).toContain("label={quickAction.label}");
    expect(installedRowBlock).not.toContain('label="Remove"');
    expect(skillsSource).toContain('return { label: "Update"');
  });

  test("ownership is progressive while loading, empty, and error reasons stay inline", () => {
    expect(skillsSource).toContain("installedSkillOwnership");
    expect(skillsSource).toContain("installedPluginOwnership");
    expect(skillsSource).toContain("SkillInspectorBody");
    expect(skillsSource).toContain("SkillInspectorSheet");
    expect(skillsSource).toContain('kind: "plugin-details"');
    expect(skillsSource).toContain("state.error");
    expect(skillsSource).toContain("catalogState.error");
    expect(skillsSource).toContain("searchState.error");
    expect(skillsSource).toContain('emptyTitle="No plugins found"');
    expect(skillsSource).toContain('loadingTitle="Loading installed Skills…"');
  });

  test("the target carousel is replaced by one options sheet with the official-icon target control", () => {
    expect(skillsSource).not.toContain("AgentSelector");
    expect(skillsSource).not.toMatch(/\shorizontal(?:\s|=)/);
    expect(skillsSource).toContain("compactSkillTargets(agentCounts)");
    // Target, Ranking, and Discovery live inside one options sheet opened by a
    // single neutral icon button; none render as top toolbar labels.
    expect(skillsSource).toContain('accessibilityLabel="Skills options"');
    expect(skillsSource).toContain("SheetSectionHeading>Target</SheetSectionHeading>");
    expect(skillsSource).toContain("SheetSectionHeading>Ranking</SheetSectionHeading>");
    expect(skillsSource).toContain("SheetSectionHeading>Discovery</SheetSectionHeading>");
    expect(skillsSource).toContain("managedAgentKind(agent)");
  });

  test("one Plugin row gets one contextual trailing action and a client icon", () => {
    const rowBlock = skillsSource.match(
      /function InstalledPluginItem\([\s\S]*?function AvailablePluginItem\(/,
    )?.[0];
    expect(rowBlock).toBeDefined();
    expect(rowBlock).toContain("AgentKindIcon");
    expect(rowBlock).toContain("ellipsis-horizontal");
    expect(rowBlock?.match(/<Pressable/g)).toHaveLength(1);
    expect(rowBlock).toContain("ItemActionIndicator");
    expect(rowBlock).not.toContain("ItemIconAction");
    expect(rowBlock).not.toContain('label="Update"');
    expect(rowBlock).not.toContain("trash-outline");
  });

  test("tabs, sheet choices, and decorative icons have one explicit accessibility owner", () => {
    const tabsBlock = skillsSource.match(
      /function SurfaceTabs\([\s\S]*?function CompactToolbar\(/,
    )?.[0];
    const sheetOptionBlock = skillsSource.match(
      /function SheetOption\([\s\S]*?function SheetAction\(/,
    )?.[0];
    const indicatorBlock = skillsSource.match(
      /function ItemActionIndicator\([\s\S]*?function InstalledCheck\(/,
    )?.[0];
    expect(tabsBlock).toContain("accessibilityLabel={tab.label}");
    expect(tabsBlock).toContain("accessible={false}");
    expect(sheetOptionBlock).toContain(
      'accessibilityLabel={[label, detail].filter(Boolean).join(", ")}',
    );
    expect(sheetOptionBlock).toContain(
      'importantForAccessibility="no-hide-descendants"',
    );
    expect(indicatorBlock).toContain("accessible={false}");
    expect(indicatorBlock).not.toContain("Pressable");
    expect(skillsSource).not.toContain("FallbackPlugin");
  });

  test("primary lists have no repeated ownership pills or placeholder grid icons", () => {
    expect(skillsSource).not.toContain("StatusBadge");
    expect(skillsSource).not.toContain('"Read-only"');
    expect(skillsSource).not.toContain('"Unmanaged"');
    expect(skillsSource).not.toContain('name="apps"');
    expect(skillsSource).not.toContain('name="grid"');
  });

  test("narrow and large-type layout wraps with 44pt controls and two-line metadata", () => {
    expect(skillsSource).toContain('flexWrap: "wrap"');
    expect(skillsSource).toContain("PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER");
    expect(skillsSource).toContain("numberOfLines={2}");
    for (const key of ["surfaceTab", "iconButton", "smallAction", "sheetOptionIcon"]) {
      const block = skillsSource.match(
        new RegExp(`${key}: \\{[\\s\\S]*?\\},`),
      )?.[0];
      expect(block, `${key} style block`).toBeDefined();
      expect(block, `${key} touch target`).toContain(
        "PLUGINS_SKILLS_TOUCH_TARGET",
      );
    }
  });
});
