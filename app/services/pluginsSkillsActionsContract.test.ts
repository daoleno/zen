import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const screen = readFileSync(join(import.meta.dir, "../app/skills.tsx"), "utf8");
const presentation = readFileSync(
  join(import.meta.dir, "../components/skills/SkillsPresentation.tsx"),
  "utf8",
);
const websocket = readFileSync(join(import.meta.dir, "websocket.ts"), "utf8");

describe("local-only Skills surface contract", () => {
  test("contains no Skills Discovery requests or product copy", () => {
    const source = screen + presentation + websocket;
    for (const removed of [
      "skills_search",
      "skills_catalog",
      "skills.sh",
      "LeaderboardSelector",
      "CatalogSkillRow",
      'return "Discover"',
    ])
      expect(source).not.toContain(removed);
  });
  test("production route contains no demo switch or embedded demo inventory", () => {
    for (const removed of [
      "SkillsProductDemo",
      "DEMO_SKILLS",
      "demoRequested",
      "useLocalSearchParams",
    ]) {
      expect(screen).not.toContain(removed);
    }
  });
  test("uses pull refresh and no duplicate refresh button", () => {
    expect(presentation).toContain("RefreshControl");
    expect(presentation).not.toContain('accessibilityLabel="Refresh"');
  });
  test("wide panel and mobile sheet share one inspector", () => {
    expect(presentation).toContain("wide && props.inspectedCopyId");
    expect(presentation).toContain("<BottomSheetFrame");
    expect(presentation.match(/<Inspector/g)?.length).toBe(2);
  });
  test("default Skills page is compact and has no Agent tab rail", () => {
    expect(presentation).toContain('accessibilityLabel="Filter Skills"');
    expect(presentation).toContain("<FilterSheet");
    expect(presentation).not.toContain("onSelectAgent");
    expect(presentation).not.toContain("agentCounts");
    expect(presentation).not.toContain("Track local Skills");
  });
  test("rows open details and lifecycle actions stay in the inspector", () => {
    const rowSource = presentation.slice(
      presentation.indexOf("function SkillRow"),
      presentation.indexOf("function Inspector"),
    );
    expect(rowSource).not.toContain("Adopt");
    expect(rowSource).not.toContain("Manage with Zen");
    expect(presentation).toContain('label="Manage with Zen"');
    expect(presentation).toContain("Copies (");
    expect(presentation).toContain("Agent bindings");
  });
  test("copy-aware details preserve file and action states", () => {
    expect(screen).toContain("skillId: skill.id");
    expect(presentation).toContain("buildSkillFileTree");
    expect(presentation).toContain("Invalid JSON");
    expect(presentation).toContain("Uninstall managed copy");
    expect(screen).toContain("buildSkillsMutationConfirmation");
    expect(screen).toContain("Alert.alert");
    expect(presentation).toContain('binding.operations.includes("unbind")');
    expect(presentation).toContain('detail.capability.operations.includes("bind")');
    expect(presentation).toContain("agent.projectScope");
  });
  test("async reads and mutations remain bound to the current server context", () => {
    expect(screen).toContain("currentSkillsContext.current !== requestContext");
    expect(screen).toContain("currentServerId.current !== requestServerId");
    expect(screen).toContain("skillsContextKey");
  });
  test("Plugin catalog and lifecycle gates remain independent", () => {
    expect(screen).toContain("pluginsUnifiedView(plugins)");
    expect(screen).toContain("evaluatePluginMutation");
    expect(presentation).toContain("installedPluginActions");
    expect(presentation).toContain("evaluatePluginMutation");
    expect(screen + presentation).toContain("onInstallPlugin");
    expect(screen + presentation).toContain("onUpdatePlugin");
    expect(screen + presentation).toContain("onUninstallPlugin");
  });
  test("loading, disconnected, empty, read, binary, and large states are explicit", () => {
    for (const copy of [
      "Loading local Skills",
      "Server disconnected",
      "No local Skills",
      "Skills unavailable",
      "Binary file",
      "preview.notice",
    ])
      expect(screen + presentation).toContain(copy);
  });
});
