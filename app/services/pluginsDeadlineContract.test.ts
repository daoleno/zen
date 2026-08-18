import { describe, expect, test } from "bun:test";
import {
  PLUGINS_INVENTORY_TIMEOUT_MS,
  PLUGIN_COMMAND_TIMEOUT_MS,
} from "./pluginsDeadlines";

/**
 * Deadline coherence contract: Claude and Codex manager reads run in parallel,
 * so the daemon's combined catalog window is the 6s per-manager bound pinned in
 * daemon/skills/plugins.go. It must expire before the App request deadlines.
 */
describe("plugin request deadline coherence", () => {
  test("daemon catalog deadline precedes the App inventory deadline", () => {
    expect(6_000).toBeLessThan(PLUGINS_INVENTORY_TIMEOUT_MS);
  });

  test("daemon catalog deadline precedes the App command deadline", () => {
    expect(6_000).toBeLessThan(PLUGIN_COMMAND_TIMEOUT_MS);
  });
});
