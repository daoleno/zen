import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const screen = readFileSync(join(import.meta.dir, "../app/skills.tsx"), "utf8");
const presentation = readFileSync(
  join(import.meta.dir, "../components/skills/SkillsPresentation.tsx"),
  "utf8",
);
const pluginsPresentation = readFileSync(
  join(import.meta.dir, "../components/plugins/PluginsPresentation.tsx"),
  "utf8",
);
const skillFileBrowser = readFileSync(
  join(import.meta.dir, "../components/skills/SkillFileBrowser.tsx"),
  "utf8",
);
const agentLogoSet = readFileSync(
  join(import.meta.dir, "../components/agents/AgentLogoSet.tsx"),
  "utf8",
);
const extensionRow = readFileSync(
  join(import.meta.dir, "../components/extensions/ExtensionListRow.tsx"),
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
    expect(presentation).toContain('props.section === "skills" && wide');
    expect(presentation).toContain("<BottomSheetFrame");
    expect(presentation.match(/<Inspector/g)?.length).toBe(2);
    expect(pluginsPresentation).toContain("props.wide ? (");
    expect(pluginsPresentation.match(/<PluginInspector/g)?.length).toBe(2);
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
    expect(rowSource).toContain("openAccessibilityLabel={`Open ${skill.name}`}");
    expect(rowSource).toContain("? `Delete ${skill.name}`");
    expect(rowSource).toContain("<ExtensionListRow");
    expect(extensionRow).toContain("width: 44");
    expect(extensionRow).toContain("height: 44");
  });
  test("copy-aware details preserve files, Agents, and exact deletion", () => {
    expect(screen).toContain("skillId: skill.id");
    expect(screen).toContain("rootPath: skill.rootPath");
    expect(screen).toContain("canonicalPath: skill.canonicalPath");
    expect(screen).toContain("allowedRoot: skill.allowedRoot");
    expect(skillFileBrowser).toContain("buildSkillFileTree");
    expect(presentation).toContain("<SkillFileBrowser");
    expect(skillFileBrowser).toContain("Invalid JSON");
    expect(presentation).toContain('title="Available to"');
    expect(presentation).toContain("DeleteCopySheet");
    expect(presentation).toContain(
      'label={deleting ? "Deleting..." : "Delete Skill"}',
    );
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
      presentation.indexOf("function Action"),
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
  test("Plugin lifecycle stays copy-exact and independent from Skills", () => {
    expect(screen).toContain("pluginUninstallInput(copy)");
    expect(screen).toContain("groupLogicalPlugins");
    expect(screen).toContain("onUninstallPlugin");
    expect(screen + presentation + pluginsPresentation).not.toContain(
      "onDeletePluginSkill",
    );
    expect(pluginsPresentation).not.toContain("Install Plugin");
    expect(pluginsPresentation).not.toContain("AvailablePlugin");
    expect(pluginsPresentation).not.toContain("Discovered");
    expect(pluginsPresentation).toContain("copy.capability.canUninstall");
  });
  test("Plugins and Skills use one shared row geometry and Agent brand owner", () => {
    expect(pluginsPresentation).toContain("<ExtensionListRow");
    expect(presentation).toContain("<ExtensionListRow");
    expect(extensionRow).toContain("<AgentLogoSet");
    expect(extensionRow).toContain("maxVisible={1}");
    expect(agentLogoSet).toContain("<AgentKindIcon");
    expect(agentLogoSet).toContain('accessibilityRole="image"');
    expect(agentLogoSet).toContain('"pi"');
    expect(agentLogoSet).toContain('"opencode"');
    expect(agentLogoSet).toContain("UnknownAgent");
    expect(extensionRow).toContain("height: 88");
    expect(extensionRow).toContain("width: 44");
    expect(extensionRow).toContain("height: 44");
    expect(pluginsPresentation).toContain(
      "paddingHorizontal: PLUGINS_SKILLS_SCREEN_PADDING",
    );
    expect(presentation).toContain(
      "paddingHorizontal: PLUGINS_SKILLS_SCREEN_PADDING",
    );
    expect(pluginsPresentation).toContain(
      "list: {\n    paddingHorizontal: PLUGINS_SKILLS_SCREEN_PADDING",
    );
    expect(presentation).toContain(
      "list: {\n    paddingHorizontal: PLUGINS_SKILLS_SCREEN_PADDING",
    );
    expect(pluginsPresentation).not.toContain(
      "paddingLeft: PLUGINS_SKILLS_SCREEN_PADDING",
    );
    expect(pluginsPresentation).not.toContain("copies.length} copies");
    expect(presentation).not.toContain("copies.length} copies");
    expect(pluginsPresentation).not.toContain("pluginRowMetadata");
  });

  test("primary rows keep technical provenance out of the default list", () => {
    const pluginRow = pluginsPresentation.slice(
      pluginsPresentation.indexOf("function PluginRow"),
      pluginsPresentation.indexOf("function PluginInspector"),
    );
    const skillRow = presentation.slice(
      presentation.indexOf("function SkillRow"),
      presentation.indexOf("function DeleteCopySheet"),
    );
    for (const removed of ["marketplace", "version", "location"]) {
      expect(pluginRow).not.toContain(removed);
    }
    for (const removed of ["rootPath", "location", "scope", "copies.length}"]) {
      expect(skillRow).not.toContain(removed);
    }
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
      expect(screen + presentation + skillFileBrowser).toContain(copy);
  });
  test("Plugin-provided Skills are presented once, inside their Plugin", () => {
    // The Skills list excludes copies whose owning Plugin is installed; the
    // Plugin inspector renders them as one expandable directory.
    expect(screen).toContain("skillsOutsidePlugins");
    expect(pluginsPresentation).toContain("pluginSkillEntries");
    expect(pluginsPresentation).toContain("<PluginSkillsDirectory");
    expect(presentation).toContain("onViewSkillPlugin");
  });
  test("protected Plugins never fake an uninstall", () => {
    expect(pluginsPresentation).toContain("uninstallable.length === 0");
    expect(pluginsPresentation).toContain("can never report success");
    expect(screen).toContain("evaluatePluginUninstall(copy)");
    expect(screen).toContain("copies remain");
  });
});
