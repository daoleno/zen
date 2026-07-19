import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

describe("shared mobile Skills surface", () => {
  test("the existing drawer and root stack own Skills navigation", () => {
    const drawer = source("../components/navigation/PrimaryDrawerPanel.tsx");
    const stack = source("../app/_layout.tsx");

    expect(drawer).toContain('label="Skills"');
    expect(drawer).toContain('openRoute("/skills")');
    expect(stack).toContain('name="skills"');
  });

  test("the screen exposes explicit Installed and Discover states without polling", () => {
    const screen = source("../app/skills.tsx");

    expect(screen).toContain('type SkillsMode = "installed" | "discover"');
    expect(screen).toContain("<InstalledView");
    expect(screen).toContain("<DiscoverView");
    expect(screen).toContain("SEARCH_DEBOUNCE_MS = 350");
    expect(screen).toContain("<FlatList");
    expect(screen).not.toContain("inventory.skills.map(");
    expect(screen).toContain("cancelSkillsCatalogSearch");
    expect(screen).not.toContain("setInterval(");
  });

  test("Discover cancels its one remote search on every ownership boundary", () => {
    const screen = source("../app/skills.tsx");

    expect((screen.match(/cancelActiveSearch\(\)/g) || []).length).toBeGreaterThanOrEqual(3);
    expect(screen).toContain("normalizedQuery.length < 2");
    expect(screen).toContain("focusedRef.current = false");
    expect(screen).toContain("[cancelActiveSearch, mode, query, runSearch]");
  });

  test("one visible Terminal is created and receives only an opaque in-memory grant", () => {
    const screen = source("../app/skills.tsx");
    const controller = source(
      "../components/terminal/useGhosttyTerminalController.ts",
    );
    const handoff = source("./skillsTerminalHandoff.ts");

    expect((screen.match(/wsClient\.createSession\(/g) || []).length).toBe(1);
    expect(screen).toContain("skillsTerminalHandoff.issue(sessionKey, command)");
    expect(screen).toContain('pathname: "/terminal/[id]"');
    expect(screen).not.toContain("command: command.command");
    expect(controller.indexOf("gridOwnerRef.current?.attach(sessionId)")).toBeLessThan(
      controller.indexOf("submitSkillsTerminalHandoff("),
    );
    expect((controller.match(/sendTerminalInput\(/g) || []).length).toBe(1);
    expect(handoff).toContain("this.current = null;");
  });

  test("Terminal submission failures remain visible with exact-command recovery", () => {
    const controller = source("../components/terminal/useGhosttyTerminalController.ts");
    const output = source("../components/terminal/TerminalOutputPane.tsx");

    expect(controller).toContain("code === 'input_failed'");
    expect(output).toContain("Skills command was not submitted.");
    expect(output).toContain("Skills command submission was not confirmed.");
    expect(output).toContain("Copy command");
  });
});
