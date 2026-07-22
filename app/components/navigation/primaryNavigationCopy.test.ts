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
    expect(skillsSource).toContain("Enter at least 2 characters");
  });

  test("Skills retains judgment-critical provenance, remove impact, and error reasons", () => {
    expect(skillsSource).toContain("installedSkillProvenance");
    expect(skillsSource).toContain("affectedAgents");
    expect(skillsSource).toContain("state.error");
    expect(skillsSource).toContain("catalogState.error");
    expect(skillsSource).toContain("searchState.error");
    expect(skillsSource).toContain("View only");
  });
});
