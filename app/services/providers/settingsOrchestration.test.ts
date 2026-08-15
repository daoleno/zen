import { describe, expect, test } from "bun:test";
import { modelSupportChangeKeepsDefaultValid } from "./settingsOrchestration";
import type { ProvidersSnapshot } from "./types";

const snapshot: ProvidersSnapshot = {
  revision: 1,
  defaults: { codex: { connection_id: "a", model_id: "m1" } },
  presets: [],
  connections: [
    { id: "a", name: "Alpha", clients: ["codex"], credential_ready: true, advanced: false },
    { id: "b", name: "Beta", clients: ["codex"], credential_ready: true, advanced: false },
  ],
  models: {
    a: [{ id: "m1", available: true, source: "bundled" }],
    b: [{ id: "m2", available: true, source: "bundled" }],
  },
};

describe("future-thread default runtime policy", () => {
  test("refuses support changes that disable the selected default model", () => {
    expect(modelSupportChangeKeepsDefaultValid({
      snapshot,
      client: "codex",
      connectionId: "a",
      enabledModelIds: [],
    })).toBe(false);
    expect(modelSupportChangeKeepsDefaultValid({
      snapshot,
      client: "codex",
      connectionId: "a",
      enabledModelIds: ["m1"],
    })).toBe(true);
  });
});
