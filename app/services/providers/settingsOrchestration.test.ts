import { describe, expect, test } from "bun:test";
import {
  assertNoCredentialRetention,
  curatedCreateInput,
  customGatewayCreateInput,
  mayDiscoverAfterCredential,
  planAfterCredentialWrite,
  planSettingsProviderSwitch,
  resolveCreatedConnection,
} from "./settingsOrchestration";
import type {
  ProviderConnection,
  ProviderCredentialResult,
  ProviderPreset,
  ProviderSessionSelection,
  ProvidersSnapshot,
} from "./types";

function snapshot(partial: Partial<ProvidersSnapshot>): ProvidersSnapshot {
  return {
    revision: 1,
    connections: [],
    defaults: {},
    presets: [],
    models: {},
    ...partial,
  };
}

describe("Settings orchestration", () => {
  test("unique created-connection identity rejects ambiguous same-preset adds", () => {
    const previous = snapshot({
      connections: [
        {
          id: "c0",
          name: "DeepSeek",
          preset_id: "deepseek",
          clients: ["codex"],
          credential_ready: true,
          advanced: false,
        },
      ],
    });
    const next = snapshot({
      revision: 2,
      connections: [
        ...previous.connections,
        {
          id: "c1",
          name: "DeepSeek",
          preset_id: "deepseek",
          clients: ["codex"],
          credential_ready: false,
          advanced: false,
        },
        {
          id: "c2",
          name: "DeepSeek 2",
          preset_id: "deepseek",
          clients: ["codex"],
          credential_ready: false,
          advanced: false,
        },
      ],
    });
    expect(() =>
      resolveCreatedConnection({
        previous,
        next,
        presetId: "deepseek",
      }),
    ).toThrow(/uniquely identify/i);
  });

  test("curated input omits manual model and protocol fields", () => {
    const preset: ProviderPreset = {
      id: "deepseek",
      label: "DeepSeek",
      clients: ["codex", "claude"],
      advanced: false,
    };
    const input = curatedCreateInput(preset, "codex");
    expect(input).toEqual({ preset_id: "deepseek", client: "codex" });
    expect(input.model_id).toBeUndefined();
  });

  test("custom gateway keeps the user-provided name and the selected client", () => {
    const input = customGatewayCreateInput({
      client: "claude",
      name: "Work gateway",
      baseUrl: "https://gateway.example/v1",
    });
    expect(input.advanced).toBe(true);
    expect(input.base_url).toContain("https://");
    expect(input.client).toBe("claude");
    expect(input.name).toBe("Work gateway");
    expect(input.model_id).toBeUndefined();
  });

  test("custom gateway keeps the optional explicit upstream model when given", () => {
    const input = customGatewayCreateInput({
      client: "codex",
      name: "CF fan",
      baseUrl: "https://cf.api.fan/v1",
      modelId: "gpt-5.6-sol",
    });
    expect(input.model_id).toBe("gpt-5.6-sol");
    expect(input.advanced).toBe(true);
  });

  test("custom gateway drops a blank explicit model back to discovery-driven", () => {
    const input = customGatewayCreateInput({
      client: "codex",
      name: "CF fan",
      baseUrl: "https://cf.api.fan/v1",
      modelId: "  ",
    });
    expect(input.model_id).toBeUndefined();
  });

  test("custom gateway still rejects non-HTTP base URLs", () => {
    expect(() =>
      customGatewayCreateInput({
        client: "codex",
        name: "Bad",
        baseUrl: "file:///tmp/provider",
      }),
    ).toThrow(/HTTP or HTTPS/i);
  });

  test("custom gateway accepts an explicit private HTTP endpoint", () => {
    expect(
      customGatewayCreateInput({
        client: "codex",
        name: "LAN",
        baseUrl: "http://192.168.1.20:8080/v1",
      }).base_url,
    ).toBe("http://192.168.1.20:8080/v1");
  });

  test("create → credential → discovery ordering refuses discovery through refresh lock", () => {
    const durable: ProviderCredentialResult = {
      connection_id: "c1",
      credential_ready: true,
      persistence: {
        applied: true,
        durable: true,
        outcome: "applied",
      },
    };
    const followUp = planAfterCredentialWrite({
      connectionId: "c1",
      result: durable,
    });
    expect(followUp.kind).toBe("discover");
    expect(
      mayDiscoverAfterCredential({
        catalogRefreshRequired: false,
        followUp,
      }),
    ).toBe(true);
    expect(
      mayDiscoverAfterCredential({
        catalogRefreshRequired: true,
        followUp,
      }),
    ).toBe(false);
  });

  test("uncertain credential write forces refresh and retryable failure keeps not-ready", () => {
    const uncertain = planAfterCredentialWrite({
      connectionId: "c1",
      result: {
        connection_id: "c1",
        credential_ready: true,
        persistence: {
          applied: true,
          durable: false,
          outcome: "applied",
          warning: "dir sync",
        },
      },
    });
    expect(uncertain.kind).toBe("refresh_lock");
    const notReady = planAfterCredentialWrite({
      connectionId: "c1",
      result: {
        connection_id: "c1",
        credential_ready: false,
        persistence: {
          applied: true,
          durable: true,
          outcome: "applied",
        },
      },
    });
    expect(notReady.kind).toBe("retry_key");
  });

  test("credential values never enter projection objects", () => {
    expect(() =>
      assertNoCredentialRetention({
        connection_id: "c1",
        credential: "sk-live",
      }),
    ).toThrow(/never enter/i);
  });
});

describe("Settings-only Provider switch plan", () => {
  const alpha: ProviderConnection = {
    id: "c1",
    name: "Alpha",
    clients: ["codex"],
    credential_ready: true,
    advanced: false,
    preset_id: "custom",
  };
  const beta: ProviderConnection = {
    id: "c2",
    name: "Beta",
    clients: ["codex"],
    credential_ready: true,
    advanced: false,
    preset_id: "custom",
  };
  const catalog = snapshot({
    connections: [alpha, beta],
    models: {
      c1: [{ id: "model-a", available: true, source: "bundled" }],
      c2: [
        { id: "model-a", available: true, source: "bundled" },
        { id: "model-b", available: true, source: "bundled" },
      ],
    },
  });
  const running: ProviderSessionSelection = {
    session_id: "s1",
    client: "codex",
    connection_id: "c1",
    connection_name: "Alpha",
    model_id: "model-a",
    credential_ready: true,
    hot_switchable: true,
  };

  test("supported current model carries over to the exact preferred pair on the same Session", () => {
    const plan = planSettingsProviderSwitch({
      snapshot: catalog,
      connection: beta,
      currentSession: { agentId: "s1" },
      currentSelection: running,
    });
    expect(plan.preferredConnectionId).toBe("c2");
    expect(plan.unsupportedCurrentModel).toBe(false);
    expect(plan.carryover).toEqual({
      agentId: "s1",
      connectionId: "c2",
      modelId: "model-a",
    });
  });

  test("unsupported current model never falls back and never plans an activation", () => {
    const plan = planSettingsProviderSwitch({
      snapshot: catalog,
      connection: alpha,
      currentSession: { agentId: "s1" },
      currentSelection: { ...running, connection_id: "c2", model_id: "model-b" },
    });
    expect(plan.carryover).toBeNull();
    expect(plan.unsupportedCurrentModel).toBe(true);
  });

  test("unknown allowlist defers to the daemon (activation still planned)", () => {
    const unsynced = snapshot({ connections: [alpha, beta], models: {} });
    const plan = planSettingsProviderSwitch({
      snapshot: unsynced,
      connection: beta,
      currentSession: { agentId: "s1" },
      currentSelection: running,
    });
    expect(plan.carryover).toEqual({
      agentId: "s1",
      connectionId: "c2",
      modelId: "model-a",
    });
  });

  test("re-selecting the Session's own Provider plans no activation", () => {
    const plan = planSettingsProviderSwitch({
      snapshot: catalog,
      connection: alpha,
      currentSession: { agentId: "s1" },
      currentSelection: running,
    });
    expect(plan.carryover).toBeNull();
    expect(plan.unsupportedCurrentModel).toBe(false);
  });

  test("no current Session or selection plans the preferred Provider only", () => {
    expect(
      planSettingsProviderSwitch({
        snapshot: catalog,
        connection: beta,
        currentSession: null,
        currentSelection: running,
      }).carryover,
    ).toBeNull();
    expect(
      planSettingsProviderSwitch({
        snapshot: catalog,
        connection: beta,
        currentSession: { agentId: "s1" },
        currentSelection: null,
      }).carryover,
    ).toBeNull();
  });

  test("read-only or model-less Sessions never plan an activation", () => {
    expect(
      planSettingsProviderSwitch({
        snapshot: catalog,
        connection: beta,
        currentSession: { agentId: "s1" },
        currentSelection: { ...running, hot_switchable: false },
      }).carryover,
    ).toBeNull();
    expect(
      planSettingsProviderSwitch({
        snapshot: catalog,
        connection: beta,
        currentSession: { agentId: "s1" },
        currentSelection: { ...running, model_id: "" },
      }).carryover,
    ).toBeNull();
  });
});
