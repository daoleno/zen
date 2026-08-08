import { describe, expect, test } from "bun:test";
import {
  PLUGINS_INVENTORY_TIMEOUT_MS,
  PLUGIN_COMMAND_TIMEOUT_MS,
} from "./pluginsDeadlines";

/**
 * Deadline coherence contract: the daemon's bounded plugin catalog read
 * (defaultPluginCLITimeout = 6s, pinned in daemon/skills/plugins.go) must
 * expire before the App's plugins_inventory and plugin_command request
 * deadlines, so the App never keeps a request alive after the daemon has
 * already given up on the catalog.
 */
describe("plugin request deadline coherence", () => {
  test("daemon catalog deadline precedes the App inventory deadline", () => {
    expect(6_000).toBeLessThan(PLUGINS_INVENTORY_TIMEOUT_MS);
  });

  test("daemon catalog deadline precedes the App command deadline", () => {
    expect(6_000).toBeLessThan(PLUGIN_COMMAND_TIMEOUT_MS);
  });
});
