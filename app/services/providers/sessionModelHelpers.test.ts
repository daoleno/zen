import { describe, expect, test } from "bun:test";
import {
  activationTargetModel,
  buildActivateSessionProviderRequest,
  refetchFoundBindingNotSwitchable,
  resolveComposerModelControl,
  sessionProviderPickerGroups,
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

describe("Session Provider+Model picker inventory", () => {
  test("lists every saved Provider compatible with the Session client, grouped by Provider Name", () => {
    const groups = sessionProviderPickerGroups(snapshot, selection);
    expect(groups.map((g) => g.connectionId)).toEqual(["c1", "c3"]);
    expect(groups.map((g) => g.connectionName)).toEqual([
      "DeepSeek",
      "Not ready",
    ]);
    // The Claude-only connection is never offered to a Codex Session.
    expect(groups.some((g) => g.connectionId === "c2")).toBe(false);
  });

  test("groups are sorted deterministically by Provider Name", () => {
    const shuffled: ProvidersSnapshot = {
      ...snapshot,
      connections: [...snapshot.connections].reverse(),
    };
    const groups = sessionProviderPickerGroups(shuffled, selection);
    expect(groups.map((g) => g.connectionName)).toEqual([
      "DeepSeek",
      "Not ready",
    ]);
  });

  test("same Base URL, different keys: two stable groups with distinct ids", () => {
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
      models: {
        "gate-a": [{ id: "alpha-1", available: true, source: "bundled" }],
        "gate-b": [{ id: "beta-1", available: true, source: "bundled" }],
      },
    };
    const groups = sessionProviderPickerGroups(dual, {
      ...selection,
      connection_id: "gate-a",
      connection_name: "Alpha Gateway",
      model_id: "alpha-1",
    });
    expect(groups.map((g) => g.connectionId)).toEqual(["gate-a", "gate-b"]);
    expect(groups.map((g) => g.hostname)).toEqual([
      "gate.example.com",
      "gate.example.com",
    ]);
    // Every selectable row carries its own Provider's stable id.
    const rows = groups.flatMap((g) => g.models);
    expect(rows.map((r) => [r.connectionId, r.modelId])).toEqual([
      ["gate-a", "alpha-1"],
      ["gate-b", "beta-1"],
    ]);
    // Only the exact running pair is marked current.
    expect(rows.map((r) => r.current)).toEqual([true, false]);
  });

  test("offers only enabled+available models; other Providers' models never appear", () => {
    const groups = sessionProviderPickerGroups(snapshot, selection);
    expect(groups[0].models.map((m) => m.modelId)).toEqual([
      "deepseek-chat",
      "deepseek-reasoner",
    ]);
    expect(groups.some((g) => g.models.some((m) => m.modelId === "gone"))).toBe(
      false,
    );
    expect(groups.some((g) => g.connectionId === "c2")).toBe(false);
  });

  test("marks exactly the current (connection_id, model_id) pair", () => {
    const groups = sessionProviderPickerGroups(snapshot, selection);
    const rows = groups.flatMap((g) => g.models);
    const current = rows.filter((m) => m.current);
    expect(current).toHaveLength(1);
    expect(current[0].connectionId).toBe("c1");
    expect(current[0].modelId).toBe("deepseek-chat");
  });

  test("cross-Provider activation targets the tapped Provider's stable pair", () => {
    const groups = sessionProviderPickerGroups(snapshot, selection);
    // The current Session runs DeepSeek; the other compatible Provider is
    // "Not ready" (c3). Its rows carry c3's exact ids even though the
    // selection is on c1.
    const other = groups.find((g) => g.connectionId === "c3");
    expect(other?.models[0]).toMatchObject({
      connectionId: "c3",
      modelId: "gpt-x",
      current: false,
    });
  });

  test("uncredentialed Providers stay visible with non-selectable models", () => {
    const groups = sessionProviderPickerGroups(snapshot, selection);
    const unready = groups.find((g) => g.connectionId === "c3");
    expect(unready?.credentialReady).toBe(false);
    expect(unready?.models).toHaveLength(1);
    expect(unready?.models[0].disabled).toBe(true);
    expect(unready?.models[0].current).toBe(false);
  });

  test("empty and failed discovery are honest and never selectable", () => {
    const noDiscovery: ProvidersSnapshot = {
      ...snapshot,
      models: { c3: [] }, // c1's discovery is gone too (record replaced)
    };
    const groups = sessionProviderPickerGroups(noDiscovery, selection);
    const empty = groups.find((g) => g.connectionId === "c3");
    expect(empty?.models).toEqual([]);
    // The running Provider still shows its checked pair from the selection
    // when discovery is empty — never substituted and never hidden.
    const running = groups.find((g) => g.connectionId === "c1");
    expect(running?.models.map((m) => m.modelId)).toEqual(["deepseek-chat"]);
    expect(running?.models[0]).toMatchObject({ current: true, disabled: true });
  });

  test("current pair missing from discovery renders checked and non-selectable", () => {
    const stale: ProvidersSnapshot = {
      ...snapshot,
      models: {
        c1: [{ id: "deepseek-reasoner", available: true, source: "bundled" }],
        c3: [],
      },
    };
    const groups = sessionProviderPickerGroups(stale, selection);
    const running = groups.find((g) => g.connectionId === "c1");
    const rows = running?.models ?? [];
    expect(rows.map((m) => m.modelId)).toEqual([
      "deepseek-reasoner",
      "deepseek-chat",
    ]);
    const current = rows.find((m) => m.current);
    expect(current).toMatchObject({
      connectionId: "c1",
      modelId: "deepseek-chat",
      disabled: true,
      unavailableCurrent: true,
    });
  });

  test("current pair unavailable in discovery renders checked and non-selectable", () => {
    const stale: ProvidersSnapshot = {
      ...snapshot,
      models: {
        c1: [{ id: "deepseek-chat", available: false, source: "lkg" }],
        c3: [],
      },
    };
    const groups = sessionProviderPickerGroups(stale, selection);
    const running = groups.find((g) => g.connectionId === "c1");
    const rows = running?.models ?? [];
    expect(rows.map((m) => m.modelId)).toEqual(["deepseek-chat"]);
    expect(rows[0]).toMatchObject({
      current: true,
      disabled: true,
      unavailableCurrent: true,
    });
  });

  test("a live route whose connection left the catalog still shows its running pair", () => {
    const removed: ProvidersSnapshot = {
      ...snapshot,
      connections: [snapshot.connections[1]], // only c2 (Claude) remains
      models: {},
    };
    const groups = sessionProviderPickerGroups(removed, selection);
    const unbound = groups.find((g) => g.connectionId === "c1");
    expect(unbound?.connectionName).toBe("DeepSeek");
    expect(unbound?.models).toHaveLength(1);
    expect(unbound?.models[0]).toMatchObject({
      connectionId: "c1",
      modelId: "deepseek-chat",
      current: true,
      disabled: true,
      unavailableCurrent: true,
    });
  });

  test("empty catalog, missing selection, or missing bound connection yields no groups", () => {
    expect(sessionProviderPickerGroups(null, selection)).toEqual([]);
    expect(sessionProviderPickerGroups(snapshot, null)).toEqual([]);
  });
});

describe("Exact ID propagation", () => {
  test("long model ids survive unchanged, including the last row", () => {
    const LONG_IDS = [
      "gpt-5.6-sol",
      "gpt-5.1-codex-max-longhaul-8k-context",
      "anthropic/claude-sonnet-4-5-20250929",
      "deepseek-r1-0528-ultra",
      "openai/gpt-oss-120b-consistency",
    ];
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
    const groups = sessionProviderPickerGroups(many, {
      ...selection,
      model_id: LONG_IDS[0],
    });
    const rows = groups[0].models;
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
    const groups = sessionProviderPickerGroups(many, {
      ...selection,
      model_id: "model-0",
    });
    const rows = groups[0].models;
    expect(rows).toHaveLength(60);
    expect(new Set(rows.map((row) => row.key)).size).toBe(60);
    expect(rows[59].modelId).toBe("model-59");
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

  test("ready only when the exact Session can activate now; label shows Provider + Model", () => {
    const control = resolveComposerModelControl({
      capabilities: managedSwitch,
      connectionConnected: true,
      selection,
      refreshRequired: false,
    });
    expect(control).toEqual({
      label: "DeepSeek · deepseek-chat",
      accessibilityLabel:
        "Open model selection, deepseek-chat, DeepSeek",
    });
  });

  test("Provider identity distinguishes same-host/different-key connections", () => {
    const control = resolveComposerModelControl({
      capabilities: managedSwitch,
      connectionConnected: true,
      selection: {
        ...selection,
        connection_name: "Alpha Gateway",
        model_id: "alpha-1",
      },
      refreshRequired: false,
    });
    expect(control?.label).toBe("Alpha Gateway · alpha-1");
  });

  test("falls back to the Provider name when the model label is empty", () => {
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
