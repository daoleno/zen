import { describe, expect, test } from "bun:test";
import {
  defaultRuntimeSeedAction,
  modelSupportChangeKeepsDefaultValid,
} from "./settingsOrchestration";
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

describe("future-thread default runtime seed", () => {
  test("preserves a valid model on the same Provider", () => {
    expect(defaultRuntimeSeedAction({ snapshot, client: "codex", connectionId: "a" })).toEqual({
      kind: "preserve",
      modelId: "m1",
    });
  });

  test("requires model selection before a different Provider is persisted", () => {
    expect(defaultRuntimeSeedAction({ snapshot, client: "codex", connectionId: "b" })).toEqual({
      kind: "choose",
      models: snapshot.models.b,
    });
  });

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
