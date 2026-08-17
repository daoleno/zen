import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const drawerSource = readFileSync(join(import.meta.dir, "PrimaryDrawerPanel.tsx"), "utf8");
const skillsSource = readFileSync(join(import.meta.dir, "../skills/SkillsPresentation.tsx"), "utf8");
const settingsSource = readFileSync(join(import.meta.dir, "../../app/settings.tsx"), "utf8");

function drawerRowContractBlock(source: string): string {
  const propsStart = source.indexOf("interface DrawerRowProps");
  const propsEnd = source.indexOf("}", propsStart);
  const fnStart = source.indexOf("function DrawerRow(");
  const fnEnd = source.indexOf("\nexport function PrimaryDrawerPanel", fnStart);
  if (propsStart < 0 || propsEnd < 0 || fnStart < 0 || fnEnd < 0) throw new Error("DrawerRow contract block not found");
  return `${source.slice(propsStart, propsEnd + 1)}\n${source.slice(fnStart, fnEnd)}`;
}

describe("navigation and Skills copy density", () => {
  test("DrawerRow contract cannot express a secondary menu description", () => {
    const contract = drawerRowContractBlock(drawerSource);
    expect(contract).toContain("label: string");
    expect(contract).toContain("accessibilityLabel={label}");
    expect(contract).not.toMatch(/\bdetail\b/);
  });
  test("drawer rows and connection strip remain concise", () => {
    for (const label of ["Skills", "Stats", "Settings"]) expect(drawerSource).toContain(`label="${label}"`);
    expect(drawerSource).toContain('"Connected"');
    expect(drawerSource).toContain('"Offline"');
    expect(drawerSource).not.toContain("Installed and discover");
  });
  test("Settings does not duplicate current-server status", () => {
    expect(settingsSource).not.toContain("Current:");
    expect(settingsSource).not.toMatch(/>\s*Current\s*</);
    expect(settingsSource).toContain("connectionLabel(connectionState)");
  });
  test("Skills chrome is a compact local manager", () => {
    expect(skillsSource).toContain('placeholder="Search local Skills"');
    expect(skillsSource).toContain("RefreshControl");
    expect(skillsSource).toContain("Track local Skills");
    expect(skillsSource).not.toContain("LeaderboardSelector");
    expect(skillsSource).not.toContain("CatalogSkillRow");
  });
  test("Plugins and Skills are the only top-level sections", () => {
    expect(skillsSource).toContain('(["skills", "plugins"] as const)');
    expect(skillsSource).not.toContain("ModeSwitch");
    expect(skillsSource).not.toContain('label="Discover"');
  });
  test("inspector uses one content model for wide panel and mobile sheet", () => {
    expect(skillsSource).toContain("const WIDE_INSPECTOR = 920");
    expect(skillsSource).toContain("<BottomSheetFrame");
    expect(skillsSource).toContain("function TreeNode");
    expect(skillsSource).toContain("function Code");
  });
  test("controls remain stable touch targets and text wraps", () => {
    expect(skillsSource).toContain('flexWrap: "wrap"');
    expect(skillsSource).toContain("minHeight: 44");
    expect(skillsSource).toContain("numberOfLines={1}");
  });
});
