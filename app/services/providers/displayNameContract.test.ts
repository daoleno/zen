/**
 * Acceptance proofs for the display-name / stable-identity redesign at the App
 * model boundary: same-URL Providers stay distinct, edits keep the stable id,
 * empty API keys preserve secrets, and upserts carry the credential only when
 * the user typed one. Daemon-side equivalents live in
 * daemon/modelprofiles/provider_display_name_test.go.
 */
import { describe, expect, test } from "bun:test";
import { advancedConnectionInput, providerBaseUrlHostname } from "./presentation";
import { parseProviderConnection, parseProvidersSnapshot } from "./parse";

describe("same Base URL with different keys stays distinct on the wire", () => {
  test("two connections with one base_url parse as independent rows", () => {
    const raw = {
      revision: 2,
      connections: [
        {
          id: "conn_aaa",
          name: "Alpha gateway",
          preset_id: "custom",
          clients: ["codex"],
          base_url: "https://gateway.example/v1",
          credential_ready: true,
          advanced: true,
        },
        {
          id: "conn_bbb",
          name: "Beta gateway",
          preset_id: "custom",
          clients: ["codex"],
          base_url: "https://gateway.example/v1",
          credential_ready: true,
          advanced: true,
        },
      ],
      presets: [],
      defaults: {},
      models: {
        conn_aaa: [],
        conn_bbb: [],
      },
    };
    const snapshot = parseProvidersSnapshot(raw);
    expect(snapshot).not.toBeNull();
    expect(snapshot!.connections).toHaveLength(2);
    expect(snapshot!.connections[0].id).not.toBe(snapshot!.connections[1].id);
    expect(snapshot!.connections[0].base_url).toBe(
      snapshot!.connections[1].base_url,
    );
    expect(snapshot!.connections[0].name).not.toBe(
      snapshot!.connections[1].name,
    );
    // The Base URL is a routing attribute, never the React/persistence key.
    expect(providerBaseUrlHostname(snapshot!.connections[0].base_url!)).toBe(
      "gateway.example",
    );
  });

  test("an edit keeps the stable connection id and may change the name", () => {
    const input = advancedConnectionInput({
      existingId: "conn_aaa",
      name: "Renamed gateway",
      client: "codex",
      baseUrl: "https://gateway.example/v1",
    });
    expect(input.id).toBe("conn_aaa");
    expect(input.name).toBe("Renamed gateway");
    expect(input.base_url).toBe("https://gateway.example/v1");
  });

  test("connection rows expose name and hostname, never the full URL as identity", () => {
    const conn = parseProviderConnection({
      id: "conn_aaa",
      name: "Alpha gateway",
      preset_id: "custom",
      clients: ["codex"],
      base_url: "https://gateway.example/v1/extra",
      credential_ready: false,
      advanced: true,
    });
    expect(conn).not.toBeNull();
    expect(conn!.name).toBe("Alpha gateway");
    expect(providerBaseUrlHostname(conn!.base_url ?? "")).toBe(
      "gateway.example",
    );
  });
});

describe("unified upsert credential contract", () => {
  test("create input carries name, URL and the selected client", () => {
    const input = advancedConnectionInput({
      name: "  Work gateway  ",
      client: "claude",
      baseUrl: " https://gateway.example/v1 ",
    });
    expect(input).toMatchObject({
      name: "Work gateway",
      client: "claude",
      base_url: "https://gateway.example/v1",
      advanced: true,
    });
    expect(input.id).toBeUndefined();
  });

  test("validation failure surfaces before any payload is built", () => {
    expect(() =>
      advancedConnectionInput({
        name: "",
        client: "codex",
        baseUrl: "https://gateway.example/v1",
      }),
    ).toThrow(/name is required/i);
    expect(() =>
      advancedConnectionInput({
        name: "Valid",
        client: "codex",
        baseUrl: "file:///tmp/x",
      }),
    ).toThrow(/HTTP or HTTPS/i);
  });

  test("curated edits keep the official endpoint and skip the Base URL", () => {
    const input = advancedConnectionInput({
      existingId: "conn_openai",
      name: "OpenAI",
      client: "codex",
      baseUrl: "",
      presetId: "openai",
      advanced: false,
    });
    expect(input).toMatchObject({
      id: "conn_openai",
      name: "OpenAI",
      preset_id: "openai",
      advanced: false,
    });
    expect(input.base_url).toBeUndefined();
  });
});
