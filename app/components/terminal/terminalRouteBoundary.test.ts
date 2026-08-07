import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const appRoot = join(import.meta.dir, "../..");
const routeTreeRoot = join(appRoot, "app");
const terminalRouteDir = join(routeTreeRoot, "terminal");
const terminalScreenDir = join(import.meta.dir, "screen");

const TERMINAL_SCREEN_CLUSTER = [
  "TerminalScreenImpl.tsx",
  "TerminalScreenLayout.tsx",
  "TerminalScreenModel.ts",
  "TerminalScreenOverlays.tsx",
  "terminalPresence.ts",
  "useSessionResourceSheet.ts",
  "useSessionProviderSheet.ts",
  "useTerminalAgentIndex.ts",
  "useTerminalChromeLayout.ts",
  "useTerminalFallbackState.ts",
  "useTerminalFocusLifecycle.ts",
  "useTerminalNavigationActions.ts",
  "useTerminalRouteModel.ts",
  "useTerminalScreenAccessory.ts",
  "useTerminalScreenActions.ts",
  "useTerminalScreenChrome.ts",
  "useTerminalScreenLayoutProps.ts",
  "useTerminalScreenLifecycle.ts",
  "useTerminalScreenLocalState.ts",
  "useTerminalScreenModels.ts",
  "useTerminalScreenOverlayProps.ts",
  "useTerminalScreenStorage.ts",
  "useTerminalSessionActions.ts",
  "useTerminalThemeChrome.ts",
  "useTerminalTopBarProps.ts",
  "useTerminalViewportModel.ts",
  "useTerminalViewportProps.ts",
] as const;

function listFilesRecursive(dir: string): string[] {
  const entries = readdirSync(dir, { withFileTypes: true });
  const files: string[] = [];
  for (const entry of entries) {
    const fullPath = join(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...listFilesRecursive(fullPath));
      continue;
    }
    if (entry.isFile()) {
      files.push(fullPath);
    }
  }
  return files;
}

describe("Expo Router terminal route boundary", () => {
  test("app/app/terminal contains exactly [id].tsx", () => {
    expect(statSync(terminalRouteDir).isDirectory()).toBe(true);
    expect(readdirSync(terminalRouteDir).sort()).toEqual(["[id].tsx"]);
  });

  test("app/components/terminal/screen holds the exact 27-file support cluster", () => {
    expect(statSync(terminalScreenDir).isDirectory()).toBe(true);
    expect(readdirSync(terminalScreenDir).sort()).toEqual(
      [...TERMINAL_SCREEN_CLUSTER].sort(),
    );
    expect(TERMINAL_SCREEN_CLUSTER).toHaveLength(27);
  });

  test("route tree forbids test/spec filenames and bun:test imports", () => {
    const routeFiles = listFilesRecursive(routeTreeRoot);
    const testOrSpec = routeFiles
      .filter((path) => /\.(test|spec)\./.test(path))
      .map((path) => relative(appRoot, path))
      .sort();
    expect(testOrSpec).toEqual([]);

    const bunTestImports = routeFiles
      .filter((path) => /\.(ts|tsx|js|jsx)$/.test(path))
      .filter((path) => /bun:test/.test(readFileSync(path, "utf8")))
      .map((path) => relative(appRoot, path))
      .sort();
    expect(bunTestImports).toEqual([]);
  });

  test("every route-tree TS/TSX module has a default export", () => {
    const routeModules = listFilesRecursive(routeTreeRoot)
      .filter((path) => /\.(ts|tsx)$/.test(path))
      .sort();

    expect(routeModules.length).toBeGreaterThan(0);

    for (const path of routeModules) {
      const source = readFileSync(path, "utf8");
      expect(
        /export\s+default\b/.test(source),
        `${relative(appRoot, path)} is missing a default export`,
      ).toBe(true);
    }
  });
});
