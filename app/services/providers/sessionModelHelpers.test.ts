import { describe, expect, test } from "bun:test";
import {
  activationTargetModel,
  buildActivateSessionProviderRequest,
  currentSessionForClient,
  modelSupportedOnConnection,
  preferredProviderConnectionId,
  refetchFoundBindingNotSwitchable,
  resolveComposerModelControl,
  sessionModelRequired,
  sessionModelSheetRows,
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
      base_url: "https://api.anthropic.com",
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
  defaults: {
    codex: { connection_id: "c1", model_id: "deepseek-chat" },
  },
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

describe("Preferred Provider resolution (Settings-selected)", () => {
  test("the preferred Provider is the catalog client default", () => {
    expect(preferredProviderConnectionId(snapshot, "codex")).toBe("c1");
    expect(preferredProviderConnectionId(snapshot, "claude")).toBe("");
    expect(preferredProviderConnectionId(null, "codex")).toBe("");
    expect(preferredProviderConnectionId(snapshot, "")).toBe("");
  });

  test("an empty default means no preferred Provider (direct login)", () => {
    const direct: ProvidersSnapshot = { ...snapshot, defaults: {} };
    expect(preferredProviderConnectionId(direct, "codex")).toBe("");
  });
});

describe("Model-required truth (route provider != preferred provider)", () => {
  test("route on the preferred Provider is never model-required", () => {
    expect(
      sessionModelRequired({ snapshot, selection }),
    ).toBe(false);
  });

  test("route on another Provider is model-required", () => {
    const switched: ProvidersSnapshot = {
      ...snapshot,
      defaults: { codex: { connection_id: "c3" } },
    };
    expect(
      sessionModelRequired({ snapshot: switched, selection }),
    ).toBe(true);
  });

  test("missing catalog or selection is never model-required", () => {
    expect(sessionModelRequired({ snapshot: null, selection })).toBe(false);
    expect(sessionModelRequired({ snapshot, selection: null })).toBe(false);
  });

  test("no preferred Provider is never model-required (control hidden)", () => {
    const direct: ProvidersSnapshot = { ...snapshot, defaults: {} };
    expect(sessionModelRequired({ snapshot: direct, selection })).toBe(false);
  });
});

describe("Support admission on a connection (never a fallback)", () => {
  test("an enabled+available model is supported", () => {
    expect(
      modelSupportedOnConnection(snapshot, "c1", "deepseek-reasoner"),
    ).toBe(true);
  });

  test("a disabled model is not supported", () => {
    const disabled: ProvidersSnapshot = {
      ...snapshot,
      models: {
        ...snapshot.models,
        c1: [
          { id: "deepseek-reasoner", available: false, source: "lkg" },
        ],
      },
    };
    expect(
      modelSupportedOnConnection(disabled, "c1", "deepseek-reasoner"),
    ).toBe(false);
  });

  test("a model absent from a synced allowlist is not supported", () => {
    expect(modelSupportedOnConnection(snapshot, "c1", "gpt-5")).toBe(false);
  });

  test("an empty/unknown allowlist defers to daemon admission", () => {
    const unsynced: ProvidersSnapshot = { ...snapshot, models: {} };
    expect(modelSupportedOnConnection(unsynced, "c1", "anything")).toBeNull();
  });
});

describe("Session Model inventory (preferred Provider only)", () => {
  test("lists only the Settings-selected Provider's enabled+available models", () => {
    const rows = sessionModelSheetRows({ snapshot, selection });
    expect(rows.map((row) => row.connectionId)).toEqual(["c1", "c1"]);
    expect(rows.map((row) => row.modelId)).toEqual([
      "deepseek-chat",
      "deepseek-reasoner",
    ]);
    // Other Providers never appear — no cross-Provider inventory.
    expect(rows.some((row) => row.connectionId === "c2")).toBe(false);
    expect(rows.some((row) => row.connectionId === "c3")).toBe(false);
    expect(rows.some((row) => row.modelId === "gone")).toBe(false);
  });

  test("same Base URL, different keys: only the preferred key's models appear", () => {
    const dual: ProvidersSnapshot = {
      ...snapshot,
      connections: [
        {
          id: "gate-a",
          name: "Alpha Gateway",
          clients: ["codex"],
          credential_ready: true,
          advanced: true,
          base_url: "https://gate.example.com",
        },
        {
          id: "gate-b",
          name: "Beta Gateway",
          clients: ["codex"],
          credential_ready: true,
          advanced: true,
          base_url: "https://gate.example.com",
        },
      ],
      defaults: {
        codex: { connection_id: "gate-b", model_id: "beta-1" },
      },
      models: {
        "gate-a": [{ id: "alpha-1", available: true, source: "bundled" }],
        "gate-b": [{ id: "beta-1", available: true, source: "bundled" }],
      },
    };
    const rows = sessionModelSheetRows({
      snapshot: dual,
      selection: {
        ...selection,
        connection_id: "gate-a",
        connection_name: "Alpha Gateway",
        model_id: "alpha-1",
      },
    });
    expect(rows.map((row) => [row.connectionId, row.modelId])).toEqual([
      ["gate-b", "beta-1"],
    ]);
  });

  test("marks exactly the current (connection_id, model_id) pair", () => {
    const rows = sessionModelSheetRows({ snapshot, selection });
    const current = rows.filter((row) => row.current);
    expect(current).toHaveLength(1);
    expect(current[0].connectionId).toBe("c1");
    expect(current[0].modelId).toBe("deepseek-chat");
  });

  test("model-required state checks nothing", () => {
    const switched: ProvidersSnapshot = {
      ...snapshot,
      defaults: { codex: { connection_id: "c3", model_id: "" } },
    };
    const rows = sessionModelSheetRows({
      snapshot: switched,
      selection,
    });
    expect(rows.every((row) => !row.current)).toBe(true);
  });

  test("uncredentialed preferred Provider stays visible with non-selectable models", () => {
    const unready: ProvidersSnapshot = {
      ...snapshot,
      defaults: { codex: { connection_id: "c3", model_id: "gpt-x" } },
    };
    const rows = sessionModelSheetRows({
      snapshot: unready,
      selection: { ...selection, connection_id: "c3", model_id: "gpt-x" },
    });
    expect(rows.map((row) => row.modelId)).toEqual(["gpt-x"]);
    expect(rows[0].disabled).toBe(true);
  });

  test("empty and failed discovery are honest and never selectable", () => {
    const noDiscovery: ProvidersSnapshot = {
      ...snapshot,
      models: { c1: [] },
    };
    const rows = sessionModelSheetRows({ snapshot: noDiscovery, selection });
    // The running pair on the preferred Provider stays visible from the
    // selection itself — checked and non-selectable, never substituted.
    expect(rows.map((row) => row.modelId)).toEqual(["deepseek-chat"]);
    expect(rows[0]).toMatchObject({
      connectionId: "c1",
      modelId: "deepseek-chat",
      current: true,
      disabled: true,
      unavailableCurrent: true,
    });
  });

  test("current pair unavailable in discovery renders checked and non-selectable", () => {
    const stale: ProvidersSnapshot = {
      ...snapshot,
      models: {
        c1: [{ id: "deepseek-chat", available: false, source: "lkg" }],
      },
    };
    const rows = sessionModelSheetRows({ snapshot: stale, selection });
    expect(rows.map((row) => row.modelId)).toEqual(["deepseek-chat"]);
    expect(rows[0]).toMatchObject({
      current: true,
      disabled: true,
      unavailableCurrent: true,
    });
  });

  test("a route on a deleted connection stays honest: no inventory, model-required", () => {
    const removed: ProvidersSnapshot = {
      ...snapshot,
      connections: [snapshot.connections[1]], // only c2 (Claude) remains
      defaults: { codex: { connection_id: "c3", model_id: "" } },
      models: {},
    };
    // The route runs c1 which left the catalog; the preferred Provider c3 has
    // no synced models. Nothing is fabricated.
    expect(sessionModelRequired({ snapshot: removed, selection })).toBe(true);
    expect(sessionModelSheetRows({ snapshot: removed, selection })).toEqual([]);
  });

  test("empty catalog, missing selection, or missing preferred yields no rows", () => {
    expect(sessionModelSheetRows({ snapshot: null, selection })).toEqual([]);
    expect(sessionModelSheetRows({ snapshot, selection: null })).toEqual([]);
    const direct: ProvidersSnapshot = { ...snapshot, defaults: {} };
    expect(sessionModelSheetRows({ snapshot: direct, selection })).toEqual([]);
  });
});

describe("Exact ID propagation", () => {
  const LONG_IDS = [
    "gpt-5.6-sol",
    "gpt-5.1-codex-max-longhaul-8k-context",
    "anthropic/claude-sonnet-4-5-20250929",
    "deepseek-r1-0528-ultra",
    "openai/gpt-oss-120b-consistency",
  ];

  test("long model ids survive unchanged, including the last row", () => {
    const many: ProvidersSnapshot = {
      ...snapshot,
      models: {
        c1: LONG_IDS.map((id) => ({
          id,
          available: true,
          source: "bundled",
        })),
      },
    };
    const rows = sessionModelSheetRows({
      snapshot: many,
      selection: { ...selection, model_id: LONG_IDS[0] },
    });
    expect(rows).toHaveLength(LONG_IDS.length);
    rows.forEach((row, index) => {
      expect(row.modelId).toBe(LONG_IDS[index]);
      expect(row.label).toBe(LONG_IDS[index]);
      expect(row.connectionId).toBe("c1");
    });
    expect(rows[rows.length - 1].modelId).toBe(
      "openai/gpt-oss-120b-consistency",
    );
  });

  test("long lists produce one row per model with unique keys", () => {
    const many: ProvidersSnapshot = {
      ...snapshot,
      models: {
        c1: Array.from({ length: 60 }, (_, i) => ({
          id: `model-${i}`,
          available: true,
          source: "bundled",
        })),
      },
    };
    const rows = sessionModelSheetRows({
      snapshot: many,
      selection: { ...selection, model_id: "model-0" },
    });
    expect(rows).toHaveLength(60);
    expect(new Set(rows.map((row) => row.key)).size).toBe(60);
    expect(rows[59].modelId).toBe("model-59");
    expect(rows[59].disabled).toBe(false);
  });
});

describe("Activation target admission (old route retained on refusal)", () => {
  const choice = { connectionId: "c1", modelId: "deepseek-reasoner" };

  test("resolves the exact available pair from the catalog", () => {
    expect(activationTargetModel(snapshot, choice)?.id).toBe(
      "deepseek-reasoner",
    );
  });

  test("refuses a model the target connection does not admit", () => {
    expect(
      activationTargetModel(snapshot, { connectionId: "c1", modelId: "gone" }),
    ).toBeNull();
  });

  test("refuses a model under the wrong connection (cross-Provider mismatch)", () => {
    expect(
      activationTargetModel(snapshot, {
        connectionId: "c2",
        modelId: "deepseek-reasoner",
      }),
    ).toBeNull();
  });

  test("refuses unavailable models and unknown connections", () => {
    expect(
      activationTargetModel(snapshot, {
        connectionId: "missing",
        modelId: "deepseek-chat",
      }),
    ).toBeNull();
    expect(
      activationTargetModel(snapshot, {
        connectionId: "c1",
        modelId: "",
      }),
    ).toBeNull();
    expect(activationTargetModel(null, choice)).toBeNull();
  });

  test("refusal never invents a fallback model id", () => {
    const target = activationTargetModel(snapshot, {
      connectionId: "c1",
      modelId: "gpt-5",
    });
    expect(target).toBeNull();
  });
});

describe("Composer model control truth", () => {
  const managedSwitch = {
    structured_events: true,
    model_profile_managed: true,
    model_profile_active_switch: true,
  };

  test("ready only when the exact Session can activate now; label is Model only", () => {
    const control = resolveComposerModelControl({
      capabilities: managedSwitch,
      connectionConnected: true,
      selection,
      refreshRequired: false,
      preferredConnectionId: "c1",
    });
    expect(control).toEqual({
      label: "deepseek-chat",
      accessibilityLabel: "Open model selection, deepseek-chat",
      modelRequired: false,
      preferredConnectionId: "c1",
    });
  });

  test("never embeds a Provider name or hostname in the label", () => {
    const control = resolveComposerModelControl({
      capabilities: managedSwitch,
      connectionConnected: true,
      selection: {
        ...selection,
        connection_id: "gate-a",
        connection_name: "Alpha Gateway",
        model_id: "alpha-1",
      },
      refreshRequired: false,
      preferredConnectionId: "gate-a",
    });
    expect(control?.label).toBe("alpha-1");
    expect(control?.label).not.toContain("Alpha Gateway");
  });

  test("a pending Provider switch is an explicit model-required request", () => {
    const control = resolveComposerModelControl({
      capabilities: managedSwitch,
      connectionConnected: true,
      selection,
      refreshRequired: false,
      preferredConnectionId: "c3",
    });
    expect(control).toEqual({
      label: "Choose model",
      accessibilityLabel:
        "Choose a model. Sending is paused until a model is selected.",
      modelRequired: true,
      preferredConnectionId: "c3",
    });
  });

  test("omits when no preferred Provider exists (direct official login)", () => {
    expect(
      resolveComposerModelControl({
        capabilities: managedSwitch,
        connectionConnected: true,
        selection,
        refreshRequired: false,
        preferredConnectionId: "",
      }),
    ).toBeNull();
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
        preferredConnectionId: "c1",
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
        preferredConnectionId: "c1",
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
        preferredConnectionId: "c1",
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
        preferredConnectionId: "c1",
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
        preferredConnectionId: "c1",
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
        preferredConnectionId: "c1",
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
    expect(
      refetchFoundBindingNotSwitchable({
        activationCapable: false,
        hotSwitchable: false,
      }),
    ).toBe(false);
  });
});

describe("Current compatible routed Session for a Settings switch", () => {
  const agents = [
    {
      id: "s-codex",
      serverId: "srv",
      command: "codex",
      capabilities: {
        structured_events: true,
        model_profile_managed: true,
        model_profile_active_switch: true,
      },
    },
    {
      id: "s-claude",
      serverId: "srv",
      command: "claude",
      capabilities: {
        structured_events: true,
        model_profile_managed: true,
        model_profile_active_switch: true,
      },
    },
    {
      id: "s-readonly",
      serverId: "srv",
      command: "codex",
      capabilities: {
        structured_events: true,
        model_profile_managed: true,
        model_profile_active_switch: false,
      },
    },
    {
      id: "s-pi",
      serverId: "srv",
      command: "pi",
      capabilities: {
        structured_events: true,
        model_profile_managed: false,
        model_profile_active_switch: false,
      },
    },
  ];

  test("resolves the last-focused managed Session of the matching client", () => {
    expect(
      currentSessionForClient({
        agents,
        currentSession: { serverId: "srv", agentId: "s-codex" },
        client: "codex",
      }),
    ).toEqual({ agentId: "s-codex" });
  });

  test("never targets another client's Session", () => {
    expect(
      currentSessionForClient({
        agents,
        currentSession: { serverId: "srv", agentId: "s-claude" },
        client: "codex",
      }),
    ).toBeNull();
  });

  test("never targets read-only or unmanaged Sessions", () => {
    expect(
      currentSessionForClient({
        agents,
        currentSession: { serverId: "srv", agentId: "s-readonly" },
        client: "codex",
      }),
    ).toBeNull();
    expect(
      currentSessionForClient({
        agents,
        currentSession: { serverId: "srv", agentId: "s-pi" },
        client: "codex",
      }),
    ).toBeNull();
  });

  test("requires a current Session and a matching server epoch", () => {
    expect(
      currentSessionForClient({
        agents,
        currentSession: null,
        client: "codex",
      }),
    ).toBeNull();
    expect(
      currentSessionForClient({
        agents,
        currentSession: { serverId: "other", agentId: "s-codex" },
        client: "codex",
      }),
    ).toBeNull();
    expect(
      currentSessionForClient({
        agents: [],
        currentSession: { serverId: "srv", agentId: "s-codex" },
        client: "codex",
      }),
    ).toBeNull();
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

  test("missing fields refuse to build", () => {
    expect(() =>
      buildActivateSessionProviderRequest({
        agentId: "",
        connectionId: "c1",
        modelId: "m",
      }),
    ).toThrow();
    expect(() =>
      buildActivateSessionProviderRequest({
        agentId: "a",
        connectionId: " ",
        modelId: "m",
      }),
    ).toThrow();
  });
});
