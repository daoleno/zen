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
    const input = curatedCreateInput(preset);
    expect(input).toEqual({ preset_id: "deepseek" });
    expect(input.model_id).toBeUndefined();
  });

  test("custom gateway remains the advanced create path", () => {
    const input = customGatewayCreateInput({
      name: "Local",
      client: "codex",
      baseUrl: "https://gateway.example/v1",
      manualModelId: "my-model",
    });
    expect(input.advanced).toBe(true);
    expect(input.base_url).toContain("https://");
    expect(input.model_id).toBe("my-model");
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
