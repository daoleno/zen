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
    {
      id: "a",
      name: "Alpha",
      clients: ["codex"],
      credential_ready: true,
      advanced: false,
    },
    {
      id: "b",
      name: "Beta",
      clients: ["codex"],
      credential_ready: true,
      advanced: false,
    },
  ],
  models: {
    a: [{ id: "m1", available: true, source: "bundled" }],
    b: [{ id: "m2", available: true, source: "bundled" }],
  },
};

describe("future-thread default runtime policy", () => {
  test("chooses the target's exposed model without a picker", () => {
    expect(
      defaultRuntimeSeedAction({
        snapshot,
        client: "codex",
        connectionId: "a",
      }),
    ).toEqual({ kind: "apply", modelId: "m1" });
    expect(
      defaultRuntimeSeedAction({
        snapshot,
        client: "codex",
        connectionId: "b",
      }),
    ).toEqual({ kind: "apply", modelId: "m2" });
  });

  test("chooses the first exposed model from direct login", () => {
    expect(
      defaultRuntimeSeedAction({
        snapshot: { ...snapshot, defaults: {} },
        client: "codex",
        connectionId: "b",
      }),
    ).toEqual({ kind: "apply", modelId: "m2" });
  });

  test("does not reuse another connection's model when changing Claude default", () => {
    const action = defaultRuntimeSeedAction({
      snapshot: {
        ...snapshot,
        defaults: { claude: { connection_id: "a", model_id: "old-model" } },
        models: {
          ...snapshot.models,
          b: [{ id: "new-model", available: true, source: "manual" }],
        },
      },
      client: "claude",
      connectionId: "b",
    });
    expect(action).toEqual({ kind: "apply", modelId: "new-model" });
  });

  test("asks for discovery when the target has no available models", () => {
    expect(
      defaultRuntimeSeedAction({
        snapshot: { ...snapshot, defaults: {}, models: {} },
        client: "codex",
        connectionId: "b",
      }),
    ).toEqual({ kind: "unavailable" });
  });

  test("prefers a manual model and skips disabled models", () => {
    expect(
      defaultRuntimeSeedAction({
        snapshot: {
          ...snapshot,
          defaults: {},
          connections: [
            { ...snapshot.connections[1], manual_model_id: "manual" },
          ],
          models: {
            b: [
              { id: "disabled", available: false, source: "cached" },
              { id: "manual", available: true, source: "manual" },
              { id: "first", available: true, source: "bundled" },
            ],
          },
        },
        client: "codex",
        connectionId: "b",
      }),
    ).toEqual({ kind: "apply", modelId: "manual" });
  });

  test("keeps a valid cached model selectable when metadata is stale", () => {
    expect(
      defaultRuntimeSeedAction({
        snapshot: {
          ...snapshot,
          defaults: {},
          connections: [{ ...snapshot.connections[1], models_stale: true }],
          models: {
            ...snapshot.models,
            b: [{ id: "m2", available: true, source: "cached" }],
          },
        },
        client: "codex",
        connectionId: "b",
      }),
    ).toEqual({ kind: "apply", modelId: "m2" });
  });

  test("refuses support changes that disable the selected default model", () => {
    expect(
      modelSupportChangeKeepsDefaultValid({
        snapshot,
        client: "codex",
        connectionId: "a",
        enabledModelIds: [],
      }),
    ).toBe(false);
    expect(
      modelSupportChangeKeepsDefaultValid({
        snapshot,
        client: "codex",
        connectionId: "a",
        enabledModelIds: ["m1"],
      }),
    ).toBe(true);
  });
});
