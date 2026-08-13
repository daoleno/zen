import { describe, expect, test } from "bun:test";
import {
  assertNoCredentialRetention,
  curatedCreateInput,
  customGatewayCreateInput,
  mayDiscoverAfterCredential,
  planAfterCredentialWrite,
  resolveCreatedConnection,
} from "./settingsOrchestration";
import type {
  ProviderCredentialResult,
  ProviderPreset,
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

  test("custom gateway derives its label and persists the selected client", () => {
    const input = customGatewayCreateInput({
      client: "claude",
      baseUrl: "https://gateway.example/v1",
    });
    expect(input.advanced).toBe(true);
    expect(input.base_url).toContain("https://");
    expect(input.client).toBe("claude");
    expect(input.name).toBe("gateway.example");
    expect(input.model_id).toBeUndefined();
  });

  test("custom gateway keeps the optional explicit upstream model when given", () => {
    const input = customGatewayCreateInput({
      client: "codex",
      baseUrl: "https://cf.api.fan/v1",
      modelId: "gpt-5.6-sol",
    });
    expect(input.model_id).toBe("gpt-5.6-sol");
    expect(input.advanced).toBe(true);
  });

  test("custom gateway drops a blank explicit model back to discovery-driven", () => {
    const input = customGatewayCreateInput({
      client: "codex",
      baseUrl: "https://cf.api.fan/v1",
      modelId: "  ",
    });
    expect(input.model_id).toBeUndefined();
  });

  test("custom gateway still rejects non-HTTP base URLs", () => {
    expect(() =>
      customGatewayCreateInput({
        client: "codex",
        baseUrl: "file:///tmp/provider",
      }),
    ).toThrow(/HTTP or HTTPS/i);
  });

  test("custom gateway accepts an explicit private HTTP endpoint", () => {
    expect(
      customGatewayCreateInput({
        client: "codex",
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
