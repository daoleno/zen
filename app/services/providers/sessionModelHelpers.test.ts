import { describe, expect, test } from "bun:test";
import {
  buildActivateSessionProviderRequest,
  refetchFoundBindingNotSwitchable,
  resolveComposerModelControl,
  sessionModelPickerChoices,
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
      { id: "deepseek-reasoner", available: true, source: "bundled" },
      { id: "gone", available: false, source: "lkg" },
    ],
    c3: [{ id: "gpt-x", available: true, source: "bundled" }],
  },
};

describe("Session model picker inventory", () => {
  test("offers only the bound connection's available models", () => {
    const choices = sessionModelPickerChoices(snapshot, selection);
    expect(choices.map((c) => c.model.id)).toEqual([
      "deepseek-chat",
      "deepseek-reasoner",
    ]);
    // Other connections and unavailable models never appear.
    expect(choices.some((c) => c.connection.id === "c2")).toBe(false);
    expect(choices.some((c) => c.connection.id === "c3")).toBe(false);
    expect(choices.some((c) => c.model.id === "gone")).toBe(false);
  });

  test("marks the current selection with the checked flag", () => {
    const choices = sessionModelPickerChoices(snapshot, selection);
    const current = choices.find((c) => c.model.id === "deepseek-chat");
    expect(current?.current).toBe(true);
    expect(
      choices.find((c) => c.model.id === "deepseek-reasoner")?.current,
    ).toBe(false);
  });

  test("empty catalog or missing bound connection yields no inventory", () => {
    expect(sessionModelPickerChoices(null, selection)).toEqual([]);
    expect(
      sessionModelPickerChoices(snapshot, {
        ...selection,
        connection_id: "missing",
      }),
    ).toEqual([]);
    expect(sessionModelPickerChoices(snapshot, null)).toEqual([]);
  });

  test("bound connection with no discovered models yields no inventory", () => {
    expect(
      sessionModelPickerChoices(
        { ...snapshot, models: {} },
        selection,
      ),
    ).toEqual([]);
  });
});

describe("Composer model control truth", () => {
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
      accessibilityLabel: "Open model selection, deepseek-chat, DeepSeek",
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

describe("Refetch transition: binding lost live switching", () => {
  test("detects the acknowledged-switch Session losing hot-switchability", () => {
    expect(
      refetchFoundBindingNotSwitchable({
        activationCapable: true,
        hotSwitchable: true,
      }),
    ).toBe(false);
    expect(
      refetchFoundBindingNotSwitchable({
        activationCapable: true,
        hotSwitchable: false,
      }),
    ).toBe(true);
    // A Session that never admitted activation is not a "lost" transition;
    // the picker was already hidden for it.
    expect(
      refetchFoundBindingNotSwitchable({
        activationCapable: false,
        hotSwitchable: false,
      }),
    ).toBe(false);
  });
});

describe("Activation contract", () => {
  test("activation request is minimal and contains no generation", () => {
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
    expect(Object.keys(request)).toEqual([
      "agentId",
      "connectionId",
      "modelId",
    ]);
  });
});
