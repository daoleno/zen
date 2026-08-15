import { describe, expect, test } from "bun:test";
import {
  resolveComposerModelControl,
  runtimeChoiceForRow,
  threadRuntimeRows,
} from "./sessionModelHelpers";
import type { ThreadRuntimeSelection, ProvidersSnapshot } from "./types";

const selection: ThreadRuntimeSelection = {
  session_id: "s1",
  client: "codex",
  connection_id: "a",
  connection_name: "Alpha",
  model_id: "gpt-5.4",
  reasoning_effort: "high",
  reasoning_effort_default: "medium",
  reasoning_efforts: ["low", "medium", "high"],
  credential_ready: true,
  hot_switchable: true,
};

const snapshot: ProvidersSnapshot = {
  revision: 1,
  defaults: { codex: { connection_id: "b", model_id: "gpt-5.5" } },
  presets: [],
  connections: [
    { id: "a", name: "Alpha", clients: ["codex"], credential_ready: true, advanced: false },
    { id: "b", name: "Beta", clients: ["codex"], credential_ready: true, advanced: false },
  ],
  models: {
    a: [{ id: "gpt-5.4", available: true, source: "bundled", reasoning_effort_default: "medium", reasoning_efforts: ["low", "medium", "high"] }],
    b: [{ id: "gpt-5.5", available: true, source: "bundled", reasoning_effort_default: "medium", reasoning_efforts: ["low", "medium", "high", "xhigh"] }],
  },
};

describe("thread runtime picker model", () => {
  test("Settings default mismatch never changes acknowledged current runtime", () => {
    const rows = threadRuntimeRows({ snapshot, selection });
    expect(rows.find((row) => row.current)?.key).toBe("a:gpt-5.4");
    expect(rows.map((row) => row.connectionId)).toEqual(["a"]);
    expect(resolveComposerModelControl({
      capabilities: {
        structured_events: true,
        model_profile_managed: true,
        model_profile_active_switch: true,
      },
      connectionConnected: true,
      selection,
      refreshRequired: false,
    })?.label).toBe("gpt-5.4");
    expect(resolveComposerModelControl({
      capabilities: {
        structured_events: true,
        model_profile_managed: true,
        model_profile_active_switch: true,
      },
      connectionConnected: true,
      selection,
      refreshRequired: false,
    })?.accessibilityLabel).toContain("Open model and effect");
  });

  test("runtime choice is atomic and rejects unsupported effects", () => {
    const target = {
      key: "b:gpt-5.5",
      connectionId: "b",
      connectionName: "Beta",
      modelId: "gpt-5.5",
      label: "gpt-5.5",
      current: false,
      disabled: false,
      unsupported: false,
      unavailableCurrent: false,
      effectDefault: "medium",
      effects: ["low", "medium", "high", "xhigh"],
      currentEffect: "",
    };
    expect(runtimeChoiceForRow(target, "xhigh")).toEqual({
      connectionId: "b",
      modelId: "gpt-5.5",
      effect: "xhigh",
    });
    expect(runtimeChoiceForRow(target, "turbo")).toBeNull();
  });
});
