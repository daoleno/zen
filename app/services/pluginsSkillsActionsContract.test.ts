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
  test("uses pull refresh and no duplicate refresh button", () => {
    expect(presentation).toContain("RefreshControl");
    expect(presentation).not.toContain('accessibilityLabel="Refresh"');
  });
  test("wide panel and mobile sheet share one inspector", () => {
    expect(presentation).toContain("wide && props.inspectedName");
    expect(presentation).toContain("<BottomSheetFrame");
    expect(presentation.match(/<Inspector/g)?.length).toBe(2);
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
