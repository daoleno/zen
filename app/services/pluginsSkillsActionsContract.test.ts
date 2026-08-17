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
  test("rows expose a separate 44px exact-copy delete action", () => {
    const rowSource = presentation.slice(
      presentation.indexOf("function SkillRow"),
      presentation.indexOf("function Inspector"),
    );
    expect(rowSource).toContain('accessibilityLabel={`Open ${skill.name}`}');
    expect(rowSource).toContain('accessibilityLabel={`Delete ${skill.name}`}');
    expect(rowSource).toContain("styles.rowOpen");
    expect(rowSource).toContain("styles.rowDelete");
    expect(presentation).toContain("width: 44");
    expect(presentation).toContain("height: 44");
  });
  test("copy-aware details preserve files, Agents, and exact deletion", () => {
    expect(screen).toContain("skillId: skill.id");
    expect(screen).toContain("rootPath: skill.rootPath");
    expect(screen).toContain("canonicalPath: skill.canonicalPath");
    expect(screen).toContain("allowedRoot: skill.allowedRoot");
    expect(presentation).toContain("buildSkillFileTree");
    expect(presentation).toContain("Invalid JSON");
    expect(presentation).toContain('title="Available to"');
    expect(presentation).toContain("DeleteCopySheet");
    expect(presentation).toContain('label={deleting ? "Deleting..." : "Delete Skill"}');
    expect(screen).toContain("buildSkillsMutationConfirmation");
    expect(screen).toContain("Alert.alert");
  });
  test("multi-copy picker includes every copy and hides actions for read-only copies", () => {
    const pickerSource = presentation.slice(
      presentation.indexOf("function DeleteCopySheet"),
      presentation.indexOf("function Inspector"),
    );
    expect(pickerSource).toContain("logical?.copies");
    expect(pickerSource).toContain("copy.capability.reason");
    expect(pickerSource).toContain("{canDelete ? (");
    expect(pickerSource).not.toContain("disabled={!canDelete}");

    const copyRowSource = presentation.slice(
      presentation.indexOf("function CopyRow"),
      presentation.indexOf("function FilePreview"),
    );
    expect(copyRowSource).toContain("styles.copyContent");
    expect(copyRowSource).toContain("styles.copyLabel");
    expect(copyRowSource).toContain("numberOfLines={2}");
    expect(presentation).toContain("copyContent: { flex: 1, minWidth: 0 }");
  });
  test("production Skills UI contains no rejected management abstraction", () => {
    for (const removed of [
      "Manage with Zen",
      "Adopt",
      "Track local Skills",
      "Agent bindings",
      "managed copy",
      "binding.operations",
      "canManage",
      "copy.manager",
      "Copies need review",
    ]) {
      expect(screen + presentation).not.toContain(removed);
    }
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
