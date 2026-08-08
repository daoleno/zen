import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  parseProviderCredentialResult,
  parseProviderConnectionTestResult,
  parseProvidersSnapshot,
  parseProviderSessionSelection,
  connectionsForSession,
  modelChoicesForSession,
  defaultClientsForConnection,
  curatedConnectionInput,
  advancedConnectionInput,
  createdConnectionFromMutation,
  ProviderRequestOwner,
  blockCreateAfterAmbiguity,
  isCreateBlockedByAmbiguity,
  shouldUnlockCreateAfterAmbiguity,
  type ProvidersSnapshot,
} from "./index";

function providerSnapshot(
  overrides?: Partial<ProvidersSnapshot>,
): ProvidersSnapshot {
  return {
    revision: 1,
    connections: [
      {
        id: "c1",
        name: "DeepSeek",
        preset_id: "deepseek",
        clients: ["codex"],
        credential_ready: true,
        advanced: false,
      },
      {
        id: "c2",
        name: "Claude Gateway",
        preset_id: "custom",
        clients: ["claude"],
        credential_ready: false,
        advanced: true,
        base_url: "https://claude.example/v1",
      },
    ],
    defaults: {
      codex: { connection_id: "c1", model_id: "deepseek-chat" },
    },
    presets: [
      {
        id: "deepseek",
        label: "DeepSeek",
        clients: ["codex"],
        advanced: false,
      },
      {
        id: "custom",
        label: "Custom Gateway",
        clients: ["codex", "claude"],
        advanced: true,
      },
    ],
    models: {
      c1: [
        { id: "deepseek-chat", available: true, source: "discovered" },
        { id: "retired", available: false, source: "lkg" },
      ],
      c2: [{ id: "claude-custom", available: true, source: "manual" }],
    },
    ...overrides,
  };
}

describe("Provider DTO parse", () => {
  test("parses curated catalog without leaking secret keys", () => {
    const catalog = parseProvidersSnapshot({
      revision: 3,
      connections: [
        {
          id: "c1",
          name: "DeepSeek",
          preset_id: "deepseek",
          clients: ["codex"],
          credential_ready: true,
        },
      ],
      defaults: {
        codex: { connection_id: "c1", model_id: "deepseek-chat" },
      },
      presets: [
        { id: "deepseek", label: "DeepSeek", clients: ["codex", "claude"] },
        {
          id: "custom",
          label: "Custom Gateway",
          clients: ["codex", "claude"],
          advanced: true,
        },
      ],
      models: {
        c1: [{ id: "deepseek-chat", available: true, source: "bundled" }],
      },
    });
    expect(catalog?.revision).toBe(3);
    expect(catalog?.connections[0]?.name).toBe("DeepSeek");
    expect(catalog?.connections[0]?.base_url).toBeUndefined();
    expect(
      connectionsForSession(catalog!, {
        session_id: "tmux:@1",
        client: "codex",
        connection_id: "c1",
        connection_name: "DeepSeek",
        model_id: "deepseek-chat",
        credential_ready: true,
        hot_switchable: true,
      }),
    ).toHaveLength(1);
    expect(defaultClientsForConnection(catalog!, "c1")).toEqual(["codex"]);
  });

  test("rejects credential keys in catalog payloads", () => {
    expect(() =>
      parseProvidersSnapshot({
        revision: 1,
        connections: [
          {
            id: "c1",
            name: "X",
            clients: ["codex"],
            credential_ready: false,
            credential: "sk-should-never-parse",
          },
        ],
        defaults: {},
        presets: [],
        models: {},
      }),
    ).toThrow(/credential/i);
  });

  test("parses session selection and credential result without secrets", () => {
    const selection = parseProviderSessionSelection({
      session_id: "tmux:@1",
      client: "codex",
      connection_id: "c1",
      connection_name: "DeepSeek",
      model_id: "deepseek-chat",
      credential_ready: true,
      hot_switchable: true,
    });
    expect(selection?.connection_id).toBe("c1");

    const cred = parseProviderCredentialResult(
      {
        connection_id: "c1",
        credential_ready: true,
        persistence_outcome: "applied",
        persistence_durable: true,
      },
      "c1",
    );
    expect(cred?.credential_ready).toBe(true);
    expect((cred as any).credential).toBeUndefined();
  });

  test("credential result parser never retains secret fields", () => {
    expect(() =>
      parseProviderCredentialResult(
        {
          connection_id: "c1",
          credential_ready: true,
          persistence_outcome: "applied",
          persistence_durable: true,
          credential: "sk-leak",
        },
        "c1",
      ),
    ).toThrow(/credential/i);
  });

  test("parses a secret-free connection test result", () => {
    expect(
      parseProviderConnectionTestResult(
        { client: "codex", model_count: 7 },
        "codex",
      ),
    ).toEqual({ client: "codex", modelCount: 7 });
    expect(
      parseProviderConnectionTestResult(
        { client: "claude", model_count: 7 },
        "codex",
      ),
    ).toBeNull();
    expect(() =>
      parseProviderConnectionTestResult(
        { client: "codex", model_count: 7, credential: "sk-leak" },
        "codex",
      ),
    ).toThrow(/credential/i);
  });
});

describe("Provider Settings and Plus presentation policy", () => {
  test("custom endpoints are explicitly scoped to one client", () => {
    const snapshot = providerSnapshot();
    expect(curatedConnectionInput(snapshot.presets[0]!)).toEqual({
      preset_id: "deepseek",
    });
    const gateway = advancedConnectionInput({
      name: "Internal",
      client: "codex",
      baseUrl: "https://gateway.example/v1",
    });
    expect(gateway).toEqual({
      name: "Internal",
      preset_id: "custom",
      client: "codex",
      base_url: "https://gateway.example/v1",
      advanced: true,
    });
    expect(gateway.client).toBe("codex");
    expect(gateway.model_id).toBeUndefined();
    expect(() =>
      advancedConnectionInput({
        name: "Bad",
        client: "codex",
        baseUrl: "file:///tmp/provider",
      }),
    ).toThrow(/HTTP or HTTPS/i);
  });

  test("create credential admission requires one unambiguous new connection", () => {
    const before = providerSnapshot();
    const created = {
      ...before,
      revision: 2,
      connections: [
        ...before.connections,
        {
          id: "c3",
          name: "DeepSeek 2",
          preset_id: "deepseek",
          clients: ["codex"],
          credential_ready: false,
          advanced: false,
        },
      ],
      models: { ...before.models, c3: [] },
    };
    expect(
      createdConnectionFromMutation(before, created, "deepseek").id,
    ).toBe("c3");

    const ambiguous = {
      ...created,
      connections: [
        ...created.connections,
        {
          id: "c4",
          name: "DeepSeek 3",
          preset_id: "deepseek",
          clients: ["codex"],
          credential_ready: false,
          advanced: false,
        },
      ],
      models: { ...created.models, c4: [] },
    };
    expect(() =>
      createdConnectionFromMutation(before, ambiguous, "deepseek"),
    ).toThrow(/uniquely identify/i);
  });

  test("Plus choices filter by Session client, availability, and credential readiness", () => {
    const snapshot = providerSnapshot();
    const codexSelection = {
      session_id: "agent-a",
      client: "codex",
      connection_id: "c1",
      connection_name: "DeepSeek",
      model_id: "deepseek-chat",
      credential_ready: true,
      hot_switchable: true,
    };
    expect(
      connectionsForSession(snapshot, codexSelection).map((item) => item.id),
    ).toEqual(["c1"]);
    expect(
      modelChoicesForSession(snapshot, codexSelection).map((choice) => ({
        connection: choice.connection.id,
        model: choice.model.id,
        current: choice.current,
        disabled: choice.disabled,
      })),
    ).toEqual([
      {
        connection: "c1",
        model: "deepseek-chat",
        current: true,
        disabled: false,
      },
    ]);

    const claudeSelection = {
      ...codexSelection,
      client: "claude",
      connection_id: "c2",
      connection_name: "Claude Gateway",
      model_id: "claude-custom",
      credential_ready: false,
    };
    expect(modelChoicesForSession(snapshot, claudeSelection)).toMatchObject([
      {
        connection: { id: "c2" },
        model: { id: "claude-custom" },
        current: true,
        disabled: true,
      },
    ]);
  });
});

describe("Provider stale, reconnect, and ambiguity admission", () => {
  test("current-server rebind invalidates stale catalog and Session tokens", () => {
    const owner = new ProviderRequestOwner();
    owner.rebind("server-a", "agent-a");
    const catalog = owner.admitCatalogLoad();
    const session = owner.admitSessionLoad();
    expect(catalog.ok).toBe(true);
    expect(session.ok).toBe(true);
    if (!catalog.ok || !session.ok) return;

    owner.rebind("server-b", "agent-b");
    expect(owner.isCurrent(catalog.token)).toBe(false);
    expect(owner.isCurrent(session.token)).toBe(false);
    expect(owner.acceptCatalog(catalog.token, 99)).toBe(false);
    expect(owner.acceptSession(session.token)).toBe(false);
  });

  test("older catalog revisions cannot overwrite the applied projection", () => {
    const owner = new ProviderRequestOwner();
    owner.rebind("server-a");
    const first = owner.admitCatalogLoad();
    expect(first.ok).toBe(true);
    if (!first.ok) return;
    expect(owner.acceptCatalog(first.token, 8)).toBe(true);

    const stale = owner.admitCatalogLoad();
    expect(stale.ok).toBe(true);
    if (!stale.ok) return;
    expect(owner.acceptCatalog(stale.token, 7)).toBe(false);
  });

  test("ambiguous catalog and activation results lock until successful refresh", () => {
    const owner = new ProviderRequestOwner();
    owner.rebind("server-a", "agent-a");
    const mutation = owner.admitCatalogMutation();
    expect(mutation.ok).toBe(true);
    if (!mutation.ok) return;
    owner.settleCatalogMutation(mutation.token, { refreshRequired: true });
    expect(owner.admitCatalogMutation()).toMatchObject({ ok: false });

    const refresh = owner.admitCatalogLoad();
    expect(refresh.ok).toBe(true);
    if (!refresh.ok) return;
    expect(owner.acceptCatalog(refresh.token, 2)).toBe(true);
    expect(owner.admitCatalogMutation()).toMatchObject({ ok: true });

    // Use a fresh owner so the admitted catalog write above does not overlap.
    const sessionOwner = new ProviderRequestOwner();
    sessionOwner.rebind("server-a", "agent-a");
    const activation = sessionOwner.admitActivation();
    expect(activation.ok).toBe(true);
    if (!activation.ok) return;
    sessionOwner.settleActivation(activation.token, {
      refreshRequired: true,
    });
    expect(sessionOwner.admitActivation()).toMatchObject({ ok: false });
    const sessionRefresh = sessionOwner.admitSessionLoad();
    expect(sessionRefresh.ok).toBe(true);
    if (!sessionRefresh.ok) return;
    expect(sessionOwner.acceptSession(sessionRefresh.token)).toBe(true);
    expect(sessionOwner.admitActivation()).toMatchObject({ ok: true });
  });

  test("ambiguous ordinary create remains blocked across reconnect until a fresh list", () => {
    const blocked = blockCreateAfterAmbiguity(
      {},
      {
        serverId: "server-a",
        connectionGeneration: 3,
        listReceipt: 10,
      },
    );
    expect(
      isCreateBlockedByAmbiguity({
        blocks: blocked,
        serverId: "server-a",
        connectionGeneration: 4,
        listReceipt: 10,
        listFreshForConnection: false,
      }),
    ).toBe(true);
    expect(
      shouldUnlockCreateAfterAmbiguity({
        block: blocked["server-a"],
        connectionGeneration: 4,
        listReceipt: 11,
        listFreshForConnection: true,
      }),
    ).toBe(true);
  });
});

describe("Provider transport source contract", () => {
  test("createSession omits profile_id and Provider methods use public wire", () => {
    const source = readFileSync(
      join(import.meta.dir, "../websocket.ts"),
      "utf8",
    );
    const createIdx = source.indexOf("createSession(");
    const listDirIdx = source.indexOf("listDir(serverId: string");
    expect(createIdx).toBeGreaterThan(0);
    expect(listDirIdx).toBeGreaterThan(createIdx);
    const createBlock = source.slice(createIdx, listDirIdx);
    expect(createBlock).toContain('type: "create_session"');
    expect(createBlock).not.toContain("profile_id");
    for (const method of [
      "list_providers",
      "upsert_provider_connection",
      "delete_provider_connection",
      "set_provider_default",
      "discover_provider_models",
      "get_session_provider",
      "activate_session_provider",
      "set_provider_credential",
      "clear_provider_credential",
    ]) {
      expect(source).toContain(`"${method}"`);
    }
    for (const banned of [
      "list_model_profiles",
      "get_model_profile",
      "upsert_model_profile",
      "delete_model_profile",
      "set_model_profile_default",
      "get_session_route",
      "activate_session_route",
    ]) {
      expect(source).not.toContain(`"${banned}"`);
    }
  });
});
