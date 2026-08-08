import { describe, expect, test } from "bun:test";
import {
  activationAllowed,
  assertActivationPayloadHasNoGeneration,
  buildActivateSessionProviderRequest,
  exactCurrentModelChoice,
  filterSessionModelChoices,
  resolveComposerModelControl,
  resolveSessionModelSheetMode,
  sessionModelChoices,
} from "./sessionModelHelpers";
import type {
  ProviderSessionSelection,
  ProvidersSnapshot,
} from "./types";

const selection: ProviderSessionSelection = {
  session_id: "tmux:@1",
  client: "codex",
  connection_id: "c1",
  connection_name: "DeepSeek",
  model_id: "deepseek-chat",
  credential_ready: true,
  hot_switchable: true,
};

const snapshot: ProvidersSnapshot = {
  revision: 2,
  connections: [
    {
      id: "c1",
      name: "DeepSeek",
      clients: ["codex"],
      credential_ready: true,
      advanced: false,
      preset_id: "deepseek",
    },
    {
      id: "c2",
      name: "Claude Gateway",
      clients: ["claude"],
      credential_ready: true,
      advanced: true,
    },
    {
      id: "c3",
      name: "Not ready",
      clients: ["codex"],
      credential_ready: false,
      advanced: false,
      preset_id: "openai",
    },
  ],
  defaults: {},
  presets: [],
  models: {
    c1: [
      { id: "deepseek-chat", available: true, source: "bundled" },
      { id: "gone", available: false, source: "lkg" },
    ],
    c3: [{ id: "gpt-x", available: true, source: "bundled" }],
  },
};

describe("Plus Model helpers", () => {
  test("filters by Session client and unavailable models", () => {
    const choices = filterSessionModelChoices(
      sessionModelChoices(snapshot, selection),
    );
    expect(choices.map((c) => `${c.connection.id}:${c.model.id}`)).toEqual([
      "c1:deepseek-chat",
      "c3:gpt-x",
    ]);
    expect(choices.find((c) => c.connection.id === "c3")?.disabled).toBe(true);
  });

  test("exact current selection is marked current", () => {
    const current = exactCurrentModelChoice(
      filterSessionModelChoices(sessionModelChoices(snapshot, selection)),
    );
    expect(current?.connection.id).toBe("c1");
    expect(current?.model.id).toBe("deepseek-chat");
    expect(current?.current).toBe(true);
  });

  test("activation request omits generation", () => {
    const request = buildActivateSessionProviderRequest({
      agentId: "tmux:@1",
      connectionId: "c1",
      modelId: "deepseek-reasoner",
    });
    expect(request).toEqual({
      agentId: "tmux:@1",
      connectionId: "c1",
      modelId: "deepseek-reasoner",
    });
    assertActivationPayloadHasNoGeneration({
      type: "activate_session_provider",
      ...request,
    });
    expect(() =>
      assertActivationPayloadHasNoGeneration({
        type: "activate_session_provider",
        ...request,
        generation: 9,
      }),
    ).toThrow(/generation/);
  });

  test("managed read-only and missing capability modes", () => {
    expect(
      resolveSessionModelSheetMode({
        capabilities: {
          structured_events: true,
          model_profile_managed: false,
          model_profile_active_switch: false,
        },
        selection,
      }),
    ).toBe("hidden");
    expect(
      resolveSessionModelSheetMode({
        capabilities: {
          structured_events: true,
          model_profile_managed: true,
          model_profile_active_switch: false,
        },
        selection,
      }),
    ).toBe("managed_readonly");
    expect(
      resolveSessionModelSheetMode({
        capabilities: {
          structured_events: true,
          model_profile_managed: true,
          model_profile_active_switch: true,
        },
        selection: { ...selection, hot_switchable: false },
      }),
    ).toBe("capability_mismatch");
  });

  test("stale refresh-before-retry blocks activation", () => {
    const choice = filterSessionModelChoices(
      sessionModelChoices(snapshot, selection),
    ).find((item) => item.connection.id === "c3");
    expect(choice).toBeTruthy();
    expect(
      activationAllowed({
        mode: "active_switch",
        choice: choice!,
        refreshRequired: true,
      }),
    ).toBe(false);
  });
});

describe("Composer model control", () => {
  const managedSwitch = {
    structured_events: true,
    model_profile_managed: true,
    model_profile_active_switch: true,
  };

  test("ready only when the exact Session can activate now", () => {
    const control = resolveComposerModelControl({
      capabilities: managedSwitch,
      connectionConnected: true,
      selection,
      refreshRequired: false,
    });
    expect(control).toEqual({
      label: "deepseek-chat",
      accessibilityLabel:
        "Open model selection, deepseek-chat, DeepSeek",
    });
  });

  test("prefers a human model label and falls back to the connection name", () => {
    expect(
      resolveComposerModelControl({
        capabilities: managedSwitch,
        connectionConnected: true,
        selection: { ...selection, model_id: "" },
        refreshRequired: false,
      })?.label,
    ).toBe("DeepSeek");
  });

  test("omits when capability is unsupported", () => {
    expect(
      resolveComposerModelControl({
        capabilities: {
          structured_events: true,
          model_profile_managed: false,
          model_profile_active_switch: false,
        },
        connectionConnected: true,
        selection,
        refreshRequired: false,
      }),
    ).toBeNull();
  });

  test("omits for managed read-only Sessions", () => {
    expect(
      resolveComposerModelControl({
        capabilities: {
          structured_events: true,
          model_profile_managed: true,
          model_profile_active_switch: false,
        },
        connectionConnected: true,
        selection,
        refreshRequired: false,
      }),
    ).toBeNull();
  });

  test("omits when disconnected", () => {
    expect(
      resolveComposerModelControl({
        capabilities: managedSwitch,
        connectionConnected: false,
        selection,
        refreshRequired: false,
      }),
    ).toBeNull();
  });

  test("omits without a loaded selection", () => {
    expect(
      resolveComposerModelControl({
        capabilities: managedSwitch,
        connectionConnected: true,
        selection: null,
        refreshRequired: false,
      }),
    ).toBeNull();
  });

  test("omits when the Session selection is not hot-switchable", () => {
    expect(
      resolveComposerModelControl({
        capabilities: managedSwitch,
        connectionConnected: true,
        selection: { ...selection, hot_switchable: false },
        refreshRequired: false,
      }),
    ).toBeNull();
  });

  test("omits when a refresh is required before mutation", () => {
    expect(
      resolveComposerModelControl({
        capabilities: managedSwitch,
        connectionConnected: true,
        selection,
        refreshRequired: true,
      }),
    ).toBeNull();
  });
});
