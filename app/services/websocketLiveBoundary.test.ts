import {
  afterAll,
  beforeEach,
  describe,
  expect,
  mock,
  spyOn,
  test,
} from "bun:test";

mock.module("react-native", () => ({ Platform: { OS: "web" } }));
mock.module("./auth", () => ({
  buildAuthorizationHeader: async () => "test-authorization",
}));
mock.module("./connectionIssue", () => ({
  diagnoseConnectionIssue: async () => null,
}));

type SocketEventHandler = ((event?: unknown) => void) | null;

class FakeWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 3;
  static instances: FakeWebSocket[] = [];

  readyState = FakeWebSocket.CONNECTING;
  sent: string[] = [];
  onopen: SocketEventHandler = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: SocketEventHandler = null;
  onerror: SocketEventHandler = null;

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this);
  }

  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.();
  }

  send(value: string) {
    if (this.readyState !== FakeWebSocket.OPEN) {
      throw new Error("socket is not open");
    }
    this.sent.push(value);
  }

  receive(message: object) {
    if (this.readyState !== FakeWebSocket.OPEN) {
      throw new Error("socket is not open");
    }
    this.onmessage?.({ data: JSON.stringify(message) });
  }

  close() {
    if (this.readyState === FakeWebSocket.CLOSED) {
      return;
    }
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.();
  }
}

const originalWebSocket = globalThis.WebSocket;
Object.assign(globalThis, { WebSocket: FakeWebSocket });

const { MultiServerWebSocketClient } = await import("./websocket");

const server = {
  id: "server-a",
  name: "Server A",
  url: "ws://server-a.test/ws",
  daemonId: "daemon-a",
  daemonPublicKey: "public-key-a",
};

const secondServer = {
  id: "server-b",
  name: "Server B",
  url: "ws://server-b.test/ws",
  daemonId: "daemon-b",
  daemonPublicKey: "public-key-b",
};

async function connectClient(
  client: InstanceType<typeof MultiServerWebSocketClient>,
  targetServer = server,
) {
  const socketIndex = FakeWebSocket.instances.length;
  client.connectServer(targetServer);
  return waitForSocket(socketIndex);
}

async function waitForSocket(socketIndex: number) {
  await Promise.resolve();
  await Promise.resolve();
  const socket = FakeWebSocket.instances[socketIndex];
  if (!socket) {
    throw new Error("expected a WebSocket connection attempt");
  }
  return socket;
}

function registeredHandlerCount(
  client: InstanceType<typeof MultiServerWebSocketClient>,
) {
  const internals = client as unknown as {
    handlers: Map<string, Array<(payload: unknown) => void>>;
  };
  return Array.from(internals.handlers.values()).reduce(
    (total, handlers) => total + handlers.length,
    0,
  );
}

function providerCatalogPayload(requestId: string, revision = 1) {
  return {
    type: "providers",
    request_id: requestId,
    revision,
    connections: [
      {
        id: "deepseek-main",
        name: "DeepSeek",
        preset_id: "deepseek",
        clients: ["codex"],
        credential_ready: true,
      },
    ],
    defaults: {
      codex: {
        connection_id: "deepseek-main",
        model_id: "deepseek-chat",
      },
    },
    presets: [
      {
        id: "deepseek",
        label: "DeepSeek",
        clients: ["codex"],
      },
      {
        id: "custom",
        label: "Custom Gateway",
        clients: ["codex", "claude"],
        advanced: true,
      },
    ],
    models: {
      "deepseek-main": [
        {
          id: "deepseek-chat",
          available: true,
          source: "discovered",
        },
      ],
    },
  };
}

function providerSelection(overrides?: Record<string, unknown>) {
  return {
    session_id: "agent-a",
    client: "codex",
    connection_id: "deepseek-main",
    connection_name: "DeepSeek",
    provider_label: "DeepSeek",
    model_id: "deepseek-chat",
    credential_ready: true,
    hot_switchable: true,
    ...overrides,
  };
}

beforeEach(() => {
  FakeWebSocket.instances = [];
});

afterAll(() => {
  Object.assign(globalThis, { WebSocket: originalWebSocket });
});

describe("generic WebSocket live boundary", () => {
  test("an incomplete Link server fails closed before WebSocket construction", async () => {
    const client = new MultiServerWebSocketClient();
    client.connectServer({
      ...server,
      url: "wss://raw.link.test/ws",
      transportKind: "link",
    });
    await new Promise((resolve) => setTimeout(resolve, 20));

    expect(FakeWebSocket.instances).toHaveLength(0);
    client.disconnectAll();
  });

  test("one terminal scroll batch sends one current-session mutation", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    client.scrollTerminal(server.id, "session-a", -3);

    expect(socket.sent).toEqual([
      JSON.stringify({
        type: "terminal_scroll",
        session_id: "session-a",
        lines: -3,
      }),
    ]);
    client.disconnectAll();
  });

  test("a disconnected mutation fails and is not sent by a later open", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);

    expect(() => client.killAgent(server.id, "agent-a")).toThrow(
      "Daemon is not connected.",
    );
    expect(() => client.listWorkItems(server.id)).toThrow(
      "Daemon is not connected.",
    );
    expect(() => client.sendInput(server.id, "agent-a", "offline")).toThrow(
      "Daemon is not connected.",
    );
    expect(registeredHandlerCount(client)).toBe(0);
    expect(socket.sent).toEqual([]);

    socket.open();
    expect(socket.sent).toEqual([]);
    client.disconnectAll();
  });

  test("intentional disconnect and reconnect do not replay an operation", async () => {
    const client = new MultiServerWebSocketClient();
    const firstSocket = await connectClient(client);
    firstSocket.open();

    client.disconnectServer(server.id);
    expect(() =>
      client.sendTerminalInput(server.id, "terminal-a", "pwd\n"),
    ).toThrow("Daemon is not connected.");

    const secondSocket = await connectClient(client);
    secondSocket.open();
    expect(firstSocket.sent).toEqual([]);
    expect(secondSocket.sent).toEqual([]);
    client.disconnectAll();
  });

  test("an open socket sends one frame for one invocation", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    client.killAgent(server.id, "agent-a");

    expect(socket.sent).toEqual([
      JSON.stringify({ type: "kill_agent", agent_id: "agent-a" }),
    ]);
    client.disconnectAll();
  });

  test("structured Chat writes once now with minimal correlated responses", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const inputReceipt = client.sendInput(server.id, "agent-a", "hello");
    expect(socket.sent).toHaveLength(1);
    expect(JSON.parse(socket.sent[0]!)).toEqual({
      type: "send_input",
      request_id: inputReceipt.requestId,
      agent_id: "agent-a",
      text: "hello",
    });
    socket.receive({
      type: "input_sent",
      request_id: inputReceipt.requestId,
    });
    expect(await inputReceipt.outcome).toEqual({ kind: "sent" });

    const pendingReceipt = client.sendInput(
      server.id,
      "agent-a",
      "wait behind foreign input",
    );
    socket.receive({
      type: "input_pending",
      request_id: pendingReceipt.requestId,
    });
    expect(await pendingReceipt.outcome).toEqual({ kind: "pending" });

    const actionReceipt = client.sendAction(server.id, "agent-a", "pause");
    expect(socket.sent).toHaveLength(3);
    expect(JSON.parse(socket.sent[2]!)).toEqual({
      type: "send_action",
      request_id: actionReceipt.requestId,
      agent_id: "agent-a",
      action: "pause",
    });
    socket.receive({
      type: "action_sent",
      request_id: actionReceipt.requestId,
    });
    expect(await actionReceipt.outcome).toEqual({ kind: "sent" });
    client.disconnectAll();
  });

  test("each repeated structured input is a fresh live attempt", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const first = client.sendInput(server.id, "agent-a", "same\n");
    const second = client.sendInput(server.id, "agent-a", "same\n");
    expect(first.requestId).not.toBe(second.requestId);
    expect(socket.sent.map((frame) => JSON.parse(frame))).toEqual([
      {
        type: "send_input",
        request_id: first.requestId,
        agent_id: "agent-a",
        text: "same\n",
      },
      {
        type: "send_input",
        request_id: second.requestId,
        agent_id: "agent-a",
        text: "same\n",
      },
    ]);
    socket.receive({ type: "input_sent", request_id: first.requestId });
    socket.receive({ type: "input_sent", request_id: second.requestId });
    expect(await first.outcome).toEqual({ kind: "sent" });
    expect(await second.outcome).toEqual({ kind: "sent" });
    client.disconnectAll();
  });

  test("repeated transient reconnects never replay an earlier failed operation", async () => {
    const client = new MultiServerWebSocketClient();
    let socket = await connectClient(client);
    socket.open();
    const sockets = [socket];

    socket.close();
    expect(() => client.killAgent(server.id, "agent-old")).toThrow(
      "Daemon is not connected.",
    );

    for (let reconnect = 0; reconnect < 3; reconnect += 1) {
      const socketIndex = FakeWebSocket.instances.length;
      client.resumeReconnects();
      socket = await waitForSocket(socketIndex);
      socket.open();
      sockets.push(socket);
      expect(socket.sent).toEqual([]);
      if (reconnect < 2) {
        socket.close();
      }
    }

    expect(sockets.every((candidate) => candidate.sent.length === 0)).toBe(
      true,
    );
    client.killAgent(server.id, "agent-new");
    expect(socket.sent).toEqual([
      JSON.stringify({ type: "kill_agent", agent_id: "agent-new" }),
    ]);
    client.disconnectAll();
  });

  test("offline active-agent presence is dropped without throw or later replay", async () => {
    const client = new MultiServerWebSocketClient();

    expect(() =>
      client.setActiveAgent(server.id, "agent-without-connection"),
    ).not.toThrow();
    const firstSocket = await connectClient(client);

    expect(() => client.clearActiveAgentsExcept(null)).not.toThrow();
    expect(() =>
      client.clearActiveAgentsExcept({
        serverId: server.id,
        agentId: "agent-offline",
      }),
    ).not.toThrow();
    expect(firstSocket.sent).toEqual([]);

    firstSocket.open();
    expect(firstSocket.sent).toEqual([]);
    firstSocket.close();

    expect(() => client.clearActiveAgentsExcept(null)).not.toThrow();
    const reconnectIndex = FakeWebSocket.instances.length;
    client.resumeReconnects();
    const reconnectedSocket = await waitForSocket(reconnectIndex);
    reconnectedSocket.open();

    expect(firstSocket.sent).toEqual([]);
    expect(reconnectedSocket.sent).toEqual([]);
    client.disconnectAll();
  });

  test("an open socket sends exactly the current selected and cleared presence", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    client.clearActiveAgentsExcept({
      serverId: server.id,
      agentId: "agent-selected",
    });
    client.clearActiveAgentsExcept(null);

    expect(socket.sent.map((frame) => JSON.parse(frame))).toEqual([
      { type: "set_active_agent", agent_id: "agent-selected" },
      { type: "set_active_agent", agent_id: "" },
    ]);
    client.disconnectAll();
  });

  test("mixed connections send presence only to sockets open at call time", async () => {
    const client = new MultiServerWebSocketClient();
    const openSocket = await connectClient(client, server);
    const offlineSocket = await connectClient(client, secondServer);
    openSocket.open();

    expect(() =>
      client.clearActiveAgentsExcept({
        serverId: secondServer.id,
        agentId: "agent-offline",
      }),
    ).not.toThrow();
    expect(openSocket.sent.map((frame) => JSON.parse(frame))).toEqual([
      { type: "set_active_agent", agent_id: "" },
    ]);
    expect(offlineSocket.sent).toEqual([]);

    offlineSocket.open();
    expect(offlineSocket.sent).toEqual([]);
    client.clearActiveAgentsExcept({
      serverId: server.id,
      agentId: "agent-current",
    });

    expect(openSocket.sent.map((frame) => JSON.parse(frame))).toEqual([
      { type: "set_active_agent", agent_id: "" },
      { type: "set_active_agent", agent_id: "agent-current" },
    ]);
    expect(offlineSocket.sent.map((frame) => JSON.parse(frame))).toEqual([
      { type: "set_active_agent", agent_id: "" },
    ]);
    client.disconnectAll();
  });

  test("a disconnected Promise request rejects immediately and cleans its timer and handlers", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    const setTimeoutSpy = spyOn(globalThis, "setTimeout");
    const clearTimeoutSpy = spyOn(globalThis, "clearTimeout");

    const request = client.getBrainContext(server.id);

    expect(registeredHandlerCount(client)).toBe(0);
    expect(setTimeoutSpy).toHaveBeenCalledTimes(1);
    expect(clearTimeoutSpy).toHaveBeenCalledTimes(1);
    await expect(request).rejects.toThrow("Daemon is not connected.");
    socket.open();
    expect(socket.sent).toEqual([]);
    setTimeoutSpy.mockRestore();
    clearTimeoutSpy.mockRestore();
    client.disconnectAll();
  });

  test("a disconnected subscription fails without retaining handlers", () => {
    const client = new MultiServerWebSocketClient();

    expect(() =>
      client.subscribeCodexConversation(
        server.id,
        { targetId: "agent-a", agentId: "agent-a" },
        {
          onSnapshot: () => {},
          onDelta: () => {},
          onSyncStatus: () => {},
          onError: () => {},
        },
      ),
    ).toThrow("Daemon is not connected.");
    expect(registeredHandlerCount(client)).toBe(0);
  });

  test("subscription cleanup after disconnect is a local no-op without replay", async () => {
    const client = new MultiServerWebSocketClient();
    const firstSocket = await connectClient(client);
    firstSocket.open();
    const unsubscribe = client.subscribeCodexConversation(
      server.id,
      { targetId: "agent-a", agentId: "agent-a" },
      {
        onSnapshot: () => {},
        onDelta: () => {},
        onSyncStatus: () => {},
        onError: () => {},
      },
    );
    expect(firstSocket.sent).toHaveLength(1);

    client.disconnectServer(server.id);
    expect(() => unsubscribe()).not.toThrow();
    expect(registeredHandlerCount(client)).toBe(0);

    const secondSocket = await connectClient(client);
    secondSocket.open();
    expect(secondSocket.sent).toEqual([]);
    client.disconnectAll();
  });

  test("conversation error frames preserve server detail and leave missing display copy to Interface", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();
    const errors: Error[] = [];
    const unsubscribe = client.subscribeCodexConversation(
      server.id,
      { targetId: "agent-a", agentId: "agent-a" },
      {
        onSnapshot: () => {},
        onDelta: () => {},
        onSyncStatus: () => {},
        onError: (error) => errors.push(error),
      },
    );
    const subscription = JSON.parse(socket.sent[0]!);

    socket.receive({
      type: "error",
      request_id: subscription.request_id,
      message: "Provider supplied detail",
    });
    socket.receive({
      type: "error",
      request_id: subscription.request_id,
    });
    socket.receive({
      type: "error",
      request_id: subscription.request_id,
      message: "",
    });

    expect(errors.map((error) => error.message)).toEqual([
      "Provider supplied detail",
      "",
      "",
    ]);
    unsubscribe();
    client.disconnectAll();
  });

  test("conversation wire normalization carries only current Activity lifecycle", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();
    const snapshots: any[] = [];
    const deltas: any[] = [];
    const syncStatuses: any[] = [];
    const unsubscribe = client.subscribeCodexConversation(
      server.id,
      { targetId: "agent-a", agentId: "agent-a" },
      {
        onSnapshot: (payload) => snapshots.push(payload),
        onDelta: (payload) => deltas.push(payload),
        onSyncStatus: (payload) => syncStatuses.push(payload),
        onError: () => {},
      },
    );
    const subscription = JSON.parse(socket.sent[0]!);
    const activity = {
      id: "provider-activity-a",
      status: "running",
      started_at: "2026-07-17T10:00:00.000Z",
    };

    socket.receive({
      type: "codex_conversation_snapshot",
      request_id: subscription.request_id,
      conversation_id: "thread-a",
      revision: 1,
      conversation: {
        available: true,
        activity,
        events: [],
      },
    });
    socket.receive({
      type: "codex_conversation_delta",
      request_id: subscription.request_id,
      conversation_id: "thread-a",
      base_revision: 1,
      revision: 2,
      upserts: [],
      deletes: [],
    });
    socket.receive({
      type: "codex_conversation_delta",
      request_id: subscription.request_id,
      conversation_id: "thread-a",
      base_revision: 2,
      revision: 3,
      activity: null,
      upserts: [],
      deletes: [],
    });
    socket.receive({
      type: "codex_conversation_sync_status",
      request_id: subscription.request_id,
      conversation_id: "thread-a",
      revision: 3,
      state: "ready",
      activity,
    });

    expect(snapshots[0]?.conversation.activity).toEqual(activity);
    expect(deltas[0]?.activity).toBeUndefined();
    expect(deltas[1]?.activity).toBeNull();
    expect(syncStatuses[0]).toEqual({
      request_id: subscription.request_id,
      conversation_id: "thread-a",
      revision: 3,
      server_generation: undefined,
      state: "ready",
      reason: undefined,
      agent_id: undefined,
    });

    unsubscribe();
    client.disconnectAll();
  });
});

describe("Provider public WebSocket boundary", () => {
  test("ordinary create omits profile_id and accepts a correlated ordinary reply", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.createSession(server.id, {
      cwd: "/workspace",
      command: "codex",
      name: "Fresh",
    });
    const outbound = JSON.parse(socket.sent.at(-1)!);
    expect(outbound).toMatchObject({
      type: "create_session",
      cwd: "/workspace",
      command: "codex",
      name: "Fresh",
    });
    expect(outbound.profile_id).toBeUndefined();

    socket.receive({
      type: "session_created",
      request_id: outbound.request_id,
      agent_id: "agent-new",
    });
    await expect(pending).resolves.toEqual({
      agentId: "agent-new",
      persistence: undefined,
    });
    client.disconnectAll();
  });

  test("list_providers ignores a stale request id and returns only the correlated catalog", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.listProviders(server.id);
    const outbound = JSON.parse(socket.sent.at(-1)!);
    expect(outbound.type).toBe("list_providers");

    let settled = false;
    void pending.finally(() => {
      settled = true;
    });
    socket.receive(providerCatalogPayload("stale-request", 99));
    await Promise.resolve();
    expect(settled).toBe(false);

    socket.receive(providerCatalogPayload(outbound.request_id, 4));
    const catalog = await pending;
    expect(catalog.revision).toBe(4);
    expect(catalog.connections[0]?.id).toBe("deepseek-main");
    expect(registeredHandlerCount(client)).toBe(0);
    client.disconnectAll();
  });

  test("Provider catalog writes use revisioned public fields and parse durability", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const upsertPending = client.upsertProviderConnection(server.id, {
      operation: "create",
      revision: 3,
      connection: { preset_id: "deepseek", client: "codex" },
    });
    const upsert = JSON.parse(socket.sent.at(-1)!);
    expect(upsert).toEqual({
      type: "upsert_provider_connection",
      request_id: upsert.request_id,
      provider_connection: { preset_id: "deepseek", client: "codex" },
      revision: 3,
      operation: "create",
    });
    expect(JSON.stringify(upsert)).not.toMatch(/api_key|credential|secret/i);
    socket.receive({
      ...providerCatalogPayload(upsert.request_id, 4),
      persistence_outcome: "applied",
      persistence_durable: false,
      persistence_warning: "directory sync pending",
    });
    const upserted = await upsertPending;
    expect(upserted.snapshot.revision).toBe(4);
    expect(upserted.persistence).toMatchObject({
      applied: true,
      durable: false,
      warning: "directory sync pending",
    });

    const defaultPending = client.setProviderDefault(server.id, {
      client: "codex",
      connectionId: "deepseek-main",
      modelId: "deepseek-chat",
      revision: 4,
    });
    const setDefault = JSON.parse(socket.sent.at(-1)!);
    expect(setDefault).toMatchObject({
      type: "set_provider_default",
      client: "codex",
      executor_id: "codex",
      connection_id: "deepseek-main",
      model_id: "deepseek-chat",
      revision: 4,
    });
    expect(setDefault.profile_id).toBeUndefined();
    socket.receive({
      ...providerCatalogPayload(setDefault.request_id, 5),
      persistence_outcome: "applied",
      persistence_durable: true,
    });
    await expect(defaultPending).resolves.toMatchObject({
      snapshot: { revision: 5 },
    });

    const switchPending = client.switchProvider(server.id, {
      client: "codex",
      connectionId: "deepseek-main",
      revision: 5,
    });
    const switchProvider = JSON.parse(socket.sent.at(-1)!);
    expect(switchProvider).toEqual({
      type: "switch_provider",
      request_id: switchProvider.request_id,
      client: "codex",
      executor_id: "codex",
      connection_id: "deepseek-main",
      revision: 5,
    });
    expect(switchProvider.model_id).toBeUndefined();
    expect(switchProvider.profile_id).toBeUndefined();
    socket.receive({
      ...providerCatalogPayload(switchProvider.request_id, 6),
      persistence_outcome: "applied",
      persistence_durable: true,
    });
    await expect(switchPending).resolves.toMatchObject({
      snapshot: { revision: 6 },
      persistence: { applied: true, durable: true },
    });
    client.disconnectAll();
  });

  test("thread runtime get and set send no legacy aliases or generation", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const getPending = client.getThreadRuntime(server.id, "agent-a");
    const get = JSON.parse(socket.sent.at(-1)!);
    expect(get).toEqual({
      type: "get_thread_runtime",
      request_id: get.request_id,
      agent_id: "agent-a",
    });
    socket.receive({
      type: "thread_runtime",
      request_id: get.request_id,
      runtime: providerSelection(),
    });
    await expect(getPending).resolves.toMatchObject({
      connection_id: "deepseek-main",
      model_id: "deepseek-chat",
    });

    const activatePending = client.setThreadRuntime(server.id, {
      agentId: "agent-a",
      runtime: { connectionId: "deepseek-main", modelId: "deepseek-chat" },
    });
    const activate = JSON.parse(socket.sent.at(-1)!);
    expect(activate).toEqual({
      type: "set_thread_runtime",
      request_id: activate.request_id,
      agent_id: "agent-a",
      runtime: {
        connection_id: "deepseek-main",
        model_id: "deepseek-chat",
      },
    });
    expect(activate.generation).toBeUndefined();
    expect(activate.profile_id).toBeUndefined();
    socket.receive({
      type: "thread_runtime_set",
      request_id: activate.request_id,
      runtime: providerSelection(),
      persistence_outcome: "applied",
      persistence_durable: true,
    });
    await expect(activatePending).resolves.toMatchObject({
      runtime: {
        session_id: "agent-a",
        connection_id: "deepseek-main",
        model_id: "deepseek-chat",
      },
    });

    const defaultEffectPending = client.setThreadRuntime(server.id, {
      agentId: "agent-a",
      runtime: {
        connectionId: "deepseek-main",
        modelId: "deepseek-chat",
        useDefaultEffect: true,
      },
    });
    const defaultEffect = JSON.parse(socket.sent.at(-1)!);
    expect(defaultEffect.runtime).toEqual({
      connection_id: "deepseek-main",
      model_id: "deepseek-chat",
      use_default_effect: true,
    });
    socket.receive({
      type: "thread_runtime_set",
      request_id: defaultEffect.request_id,
      runtime: providerSelection({ reasoning_effort: undefined }),
      persistence_outcome: "applied",
      persistence_durable: true,
    });
    await expect(defaultEffectPending).resolves.toMatchObject({
      runtime: { reasoning_effort: undefined },
    });
    client.disconnectAll();
  });

  test("activation rejects a mismatched connection/model reply", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.setThreadRuntime(server.id, {
      agentId: "agent-a",
      runtime: { connectionId: "deepseek-main", modelId: "deepseek-chat" },
    });
    const outbound = JSON.parse(socket.sent.at(-1)!);
    socket.receive({
      type: "thread_runtime_set",
      request_id: outbound.request_id,
      runtime: providerSelection({
        connection_id: "other-connection",
        model_id: "other-model",
      }),
      persistence_outcome: "applied",
      persistence_durable: true,
    });
    await expect(pending).rejects.toThrow(/invalid activation selection/i);
    expect(registeredHandlerCount(client)).toBe(0);
    client.disconnectAll();
  });

  test("credential is write-only and daemon error text cannot echo it", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();
    const submittedKey = "sk-provider-test-never-echo";

    const pending = client.setProviderCredential(
      server.id,
      "deepseek-main",
      submittedKey,
    );
    const outbound = JSON.parse(socket.sent.at(-1)!);
    expect(outbound).toEqual({
      type: "set_provider_credential",
      request_id: outbound.request_id,
      connection_id: "deepseek-main",
      credential: submittedKey,
    });

    socket.receive({
      type: "error",
      request_id: outbound.request_id,
      code: "credential_store_failed",
      message: `credential ${submittedKey} failed`,
    });
    const error = (await pending.catch((reason) => reason as Error)) as Error;
    expect(error.message).not.toContain(submittedKey);
    expect(error.message).toMatch(/API key/i);
    expect(registeredHandlerCount(client)).toBe(0);
    client.disconnectAll();
  });

  test("connection test sends a transient key and retains only secret-free facts", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();
    const submittedKey = "sk-transient-test-only";

    const pending = client.testProviderConnection(server.id, {
      client: "codex",
      baseUrl: "https://gateway.example/v1",
      apiKey: submittedKey,
    });
    const outbound = JSON.parse(socket.sent.at(-1)!);
    expect(outbound).toMatchObject({
      type: "test_provider_connection",
      provider_connection: {
        preset_id: "custom",
        client: "codex",
        base_url: "https://gateway.example/v1",
        advanced: true,
      },
      credential: submittedKey,
    });

    socket.receive({
      type: "provider_connection_test",
      request_id: outbound.request_id,
      client: "codex",
      model_count: 4,
      latency_ms: 87,
    });
    const result = await pending;
    expect(result).toEqual({ client: "codex", modelCount: 4, latencyMs: 87 });
    expect(JSON.stringify(result)).not.toMatch(/credential|api.?key|secret/i);
    expect(registeredHandlerCount(client)).toBe(0);
    client.disconnectAll();
  });
});

describe("executor switch transport", () => {
  test("setDelegatedExecutor is request-correlated and returns the brain snapshot", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.setDelegatedExecutor(server.id, "grok");
    const outbound = JSON.parse(socket.sent.at(-1)!);
    expect(outbound.type).toBe("set_delegated_executor");
    expect(outbound.executor_id).toBe("grok");
    expect(outbound.adapter_id).toBe("grok");
    expect(typeof outbound.request_id).toBe("string");

    socket.receive({
      type: "brain_snapshot",
      request_id: outbound.request_id,
      brain: {
        delegated_adapter: { id: "grok", name: "Grok", provider: "grok" },
        host_adapter: { id: "codex", name: "Codex", provider: "codex" },
      },
    });

    await expect(pending).resolves.toEqual({
      delegated_adapter: { id: "grok", name: "Grok", provider: "grok" },
      host_adapter: { id: "codex", name: "Codex", provider: "codex" },
    });
    client.disconnectAll();
  });

  test("setBrainExecutor and setDelegatedExecutor stay on distinct operations", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const hostPending = client.setBrainExecutor(server.id, "claude");
    const hostOutbound = JSON.parse(socket.sent.at(-1)!);
    const delegatedPending = client.setDelegatedExecutor(server.id, "grok");
    const delegatedOutbound = JSON.parse(socket.sent.at(-1)!);

    expect(hostOutbound.type).toBe("brain_set_executor");
    expect(delegatedOutbound.type).toBe("set_delegated_executor");
    expect(hostOutbound.request_id).not.toBe(delegatedOutbound.request_id);

    socket.receive({
      type: "brain_snapshot",
      request_id: hostOutbound.request_id,
      brain: { host_adapter: { id: "claude", name: "Claude" } },
    });
    socket.receive({
      type: "brain_snapshot",
      request_id: delegatedOutbound.request_id,
      brain: { delegated_adapter: { id: "grok", name: "Grok" } },
    });

    await expect(hostPending).resolves.toEqual({
      host_adapter: { id: "claude", name: "Claude" },
    });
    await expect(delegatedPending).resolves.toEqual({
      delegated_adapter: { id: "grok", name: "Grok" },
    });
    client.disconnectAll();
  });

  test("setDelegatedExecutor rejects only the matching request error", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.setDelegatedExecutor(server.id, "missing");
    const outbound = JSON.parse(socket.sent.at(-1)!);

    socket.receive({
      type: "error",
      request_id: "other-request",
      message: "ignored",
    });
    socket.receive({
      type: "error",
      request_id: outbound.request_id,
      code: "invalid_executor",
      message: "unknown executor: missing",
    });

    await expect(pending).rejects.toThrow("unknown executor: missing");
    client.disconnectAll();
  });
});

describe("Skills management transport", () => {
  const identity = {
    operation: "delete" as const,
    skillId: "a".repeat(24),
    skillName: "useful",
    rootPath: "/home/test/.codex/skills/useful",
    canonicalPath: "/home/test/.codex/skills/useful",
    allowedRoot: "/home/test/.codex/skills",
    cwd: "/workspace/project",
  };

  test("inventory is request-correlated and generation-safe", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.getSkillsInventory(server.id, {
      cwd: "/workspace/project",
      generation: 4,
    });
    const outbound = JSON.parse(socket.sent.at(-1)!);
    expect(outbound).toMatchObject({
      type: "skills_inventory",
      cwd: "/workspace/project",
      generation: 4,
    });
    expect(typeof outbound.request_id).toBe("string");

    socket.receive({
      type: "skills_inventory",
      request_id: "other-request",
      generation: 4,
      inventory: {},
    });
    socket.receive({
      type: "skills_inventory",
      request_id: outbound.request_id,
      generation: 4,
      inventory: {
        generated_at: "2026-07-19T00:00:00Z",
        cwd: "/workspace/project",
        skills: [],
        agents: [],
        executors: [],
        warnings: [],
        mutation_operations: ["delete"],
      },
    });

    await expect(pending).resolves.toEqual({
      generation: 4,
      inventory: {
        generatedAt: "2026-07-19T00:00:00Z",
        cwd: "/workspace/project",
        skills: [],
        agents: [],
        warnings: [],
        mutationOperations: ["delete"],
      },
    });
    client.disconnectAll();
  });

  test("inspect rejects a valid but mismatched copy identity", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.getSkillsInspect(server.id, {
      skillName: "useful",
      skillId: "c".repeat(24),
      generation: 7,
    });
    const outbound = JSON.parse(socket.sent.at(-1)!);
    socket.receive({
      type: "skills_inspect_result",
      request_id: outbound.request_id,
      generation: 7,
      detail: {
        copy_id: "d".repeat(24),
        skill_name: "useful",
        enabled: true,
        root_path: identity.rootPath,
        canonical_path: identity.canonicalPath,
        allowed_root: identity.allowedRoot,
        location: "Codex global Skills",
        scope: "global",
        agents: ["codex"],
        capability: { can_delete: true },
      },
    });

    await expect(pending).rejects.toThrow("different Skill copy");
    client.disconnectAll();
  });

  test("a reviewed delete command for a different exact copy is rejected", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.buildSkillsCommand(server.id, identity);
    const outbound = JSON.parse(socket.sent.at(-1)!);
    expect(outbound).toMatchObject({
      type: "skills_command",
      operation: "delete",
      skill_id: identity.skillId,
      skill_name: identity.skillName,
      root_path: identity.rootPath,
      canonical_path: identity.canonicalPath,
      allowed_root: identity.allowedRoot,
    });
    socket.receive({
      type: "skills_command",
      request_id: outbound.request_id,
      command: {
        operation: "delete",
        scope: "global",
        agents: ["codex"],
        skill_name: "useful",
        copy_id: identity.skillId,
        root_path: "/home/test/.codex/skills/other-copy",
        canonical_path: identity.canonicalPath,
        allowed_root: identity.allowedRoot,
        location: "Codex global Skills",
        summary: "Delete useful",
        destructive: true,
      },
    });

    await expect(pending).rejects.toThrow("different copy");
    client.disconnectAll();
  });

  test("delete accepts daemon-derived affected Agents after exact identity review", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.buildSkillsCommand(server.id, identity);
    const outbound = JSON.parse(socket.sent.at(-1)!);
    socket.receive({
      type: "skills_command",
      request_id: outbound.request_id,
      command: {
        operation: "delete",
        scope: "global",
        agents: ["codex", "pi"],
        skill_name: identity.skillName,
        copy_id: identity.skillId,
        root_path: identity.rootPath,
        canonical_path: identity.canonicalPath,
        allowed_root: identity.allowedRoot,
        location: "Shared Agent Skills",
        summary: "Delete useful from Shared Agent Skills",
        destructive: true,
      },
    });

    await expect(pending).resolves.toMatchObject({
      operation: "delete",
      agents: ["codex", "pi"],
    });
    client.disconnectAll();
  });

  test("delete accepts the daemon top-level mutation result", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.executeSkillsMutation(server.id, identity);
    const outbound = JSON.parse(socket.sent.at(-1)!);
    expect(outbound).toMatchObject({
      type: "skills_mutation",
      operation: "delete",
      skill_id: identity.skillId,
      skill_name: identity.skillName,
      root_path: identity.rootPath,
      canonical_path: identity.canonicalPath,
      allowed_root: identity.allowedRoot,
    });
    socket.receive({
      type: "skills_mutation_result",
      request_id: outbound.request_id,
      command: {
        operation: "delete",
        scope: "global",
        agents: ["codex"],
        skill_name: identity.skillName,
        copy_id: identity.skillId,
        root_path: identity.rootPath,
        canonical_path: identity.canonicalPath,
        allowed_root: identity.allowedRoot,
        location: "Codex global Skills",
        summary: "Delete useful from Codex global Skills",
        destructive: true,
      },
      success: true,
      exit_code: 0,
      output: "Deleted useful.",
      duration_ms: 12,
    });

    await expect(pending).resolves.toEqual({
      command: expect.objectContaining({
        copyId: identity.skillId,
        skillName: identity.skillName,
      }),
      execution: {
        success: true,
        exitCode: 0,
        output: "Deleted useful.",
        durationMs: 12,
      },
    });
    client.disconnectAll();
  });
});

describe("Plugin management transport", () => {
  const identity = {
    operation: "uninstall" as const,
    pluginId: "temporary@personal",
    host: "codex" as const,
    source: "manager" as const,
    scope: "user" as const,
    copyId: "b".repeat(24),
    name: "temporary",
    version: "1.0.0",
    rootPath:
      "/home/test/.codex/plugins/cache/personal/temporary/1.0.0",
    canonicalPath:
      "/home/test/.codex/plugins/cache/personal/temporary/1.0.0",
    allowedRoot: "/home/test/.codex/plugins/cache/personal/temporary",
    revision: "c".repeat(64),
    agents: ["codex" as const],
  };

  const command = {
    operation: "uninstall",
    plugin_id: identity.pluginId,
    host: identity.host,
    source: identity.source,
    scope: identity.scope,
    copy_id: identity.copyId,
    name: identity.name,
    display_name: "Temporary",
    version: identity.version,
    root_path: identity.rootPath,
    canonical_path: identity.canonicalPath,
    allowed_root: identity.allowedRoot,
    location: "Codex user Plugins",
    revision: identity.revision,
    agents: identity.agents,
    summary: "Permanently uninstall Temporary from Codex user Plugins",
    destructive: true,
  };

  test("inventory is request-correlated and generation-safe", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.getPluginsInventory(server.id, { generation: 9 });
    const outbound = JSON.parse(socket.sent.at(-1)!);
    socket.receive({
      type: "plugins_inventory",
      request_id: "other-request",
      generation: 9,
      inventory: {},
    });
    socket.receive({
      type: "plugins_inventory",
      request_id: outbound.request_id,
      generation: 9,
      inventory: {
        generated_at: "2026-08-18T00:00:00Z",
        installed: [],
        available: [],
        warnings: [],
      },
    });

    await expect(pending).resolves.toMatchObject({
      generation: 9,
      inventory: { installed: [], available: [] },
    });
    client.disconnectAll();
  });

  test("review sends and verifies the complete exact-copy identity", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.buildPluginCommand(server.id, identity);
    const outbound = JSON.parse(socket.sent.at(-1)!);
    expect(outbound).toMatchObject({
      type: "plugin_command",
      plugin_id: identity.pluginId,
      plugin_host: identity.host,
      plugin_source: identity.source,
      plugin_version: identity.version,
      plugin_copy_id: identity.copyId,
      plugin_name: identity.name,
      root_path: identity.rootPath,
      canonical_path: identity.canonicalPath,
      allowed_root: identity.allowedRoot,
      plugin_revision: identity.revision,
      agents: identity.agents,
    });
    socket.receive({
      type: "plugin_command",
      request_id: outbound.request_id,
      command,
    });

    await expect(pending).resolves.toMatchObject({
      copyId: identity.copyId,
      source: identity.source,
      version: identity.version,
      agents: identity.agents,
    });
    client.disconnectAll();
  });

  test("mutation accepts a top-level result and rejects identity drift", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.executePluginMutation(server.id, identity);
    const outbound = JSON.parse(socket.sent.at(-1)!);
    socket.receive({
      type: "plugin_mutation_result",
      request_id: outbound.request_id,
      command: { ...command, revision: "d".repeat(64) },
      success: true,
      exit_code: 0,
      output: "Uninstalled Temporary.",
      duration_ms: 10,
    });

    await expect(pending).rejects.toThrow("different Plugin copy");
    client.disconnectAll();
  });

  test("mutation returns truthful loading success and failure outcomes", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.executePluginMutation(server.id, identity);
    const outbound = JSON.parse(socket.sent.at(-1)!);
    socket.receive({
      type: "plugin_mutation_result",
      request_id: outbound.request_id,
      command,
      success: true,
      exit_code: 0,
      output: "Uninstalled Temporary.",
      duration_ms: 10,
    });

    await expect(pending).resolves.toMatchObject({
      command: { copyId: identity.copyId, revision: identity.revision },
      execution: { success: true, exitCode: 0 },
    });
    client.disconnectAll();
  });
});

describe("structured input identity reuse", () => {
  test("a retry reuses its stable request id while a new input stays fresh", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const retry = client.sendInput(server.id, "agent-a", "same\n", {
      displayBody: "same",
      requestId: "request-stable",
    });
    expect(retry.requestId).toBe("request-stable");
    const next = client.sendInput(server.id, "agent-a", "different\n");
    expect(next.requestId).not.toBe("request-stable");
    expect(socket.sent.map((frame) => JSON.parse(frame))).toEqual([
      {
        type: "send_input",
        request_id: "request-stable",
        agent_id: "agent-a",
        text: "same\n",
        display_body: "same",
      },
      {
        type: "send_input",
        request_id: next.requestId,
        agent_id: "agent-a",
        text: "different\n",
      },
    ]);
    socket.receive({ type: "input_sent", request_id: "request-stable" });
    socket.receive({ type: "input_sent", request_id: next.requestId });
    expect(await retry.outcome).toEqual({ kind: "sent" });
    expect(await next.outcome).toEqual({ kind: "sent" });
    client.disconnectAll();
  });

  test("late ack for a reused identity settles only that logical input", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const first = client.sendInput(server.id, "agent-a", "same\n", {
      requestId: "request-reused",
    });
    socket.receive({ type: "input_failed", request_id: "request-reused" });
    expect(await first.outcome).toEqual({
      kind: "failed",
      failure: {
        requestId: "request-reused",
        code: "command_failed",
        message: "The provider command failed.",
      },
    });
    const second = client.sendInput(server.id, "agent-a", "same\n", {
      requestId: "request-reused",
    });
    // A late duplicate failure for the same identity settles the retry
    // outcome; the daemon correlates by the stable identity, never by
    // attempt order.
    socket.receive({ type: "input_sent", request_id: "request-reused" });
    expect(await second.outcome).toEqual({ kind: "sent" });
    client.disconnectAll();
  });
});

describe("Telegram current-daemon connection boundary", () => {
  test("status and configuration correlate to the requested daemon", async () => {
    const client = new MultiServerWebSocketClient();
    const first = await connectClient(client, server);
    const second = await connectClient(client, secondServer);
    first.open();
    second.open();

    const pending = client.configureTelegramConnection(
      server.id,
      "fixture-token-never-returned",
    );
    const outbound = JSON.parse(first.sent.at(-1)!);
    expect(outbound).toMatchObject({
      type: "telegram_connection_configure",
      credential: "fixture-token-never-returned",
    });

    second.receive({
      type: "telegram_connection_status",
      request_id: outbound.request_id,
      connection: { state: "connected", enabled: true },
    });
    await Promise.resolve();
    expect(registeredHandlerCount(client)).toBeGreaterThan(0);

    first.receive({
      type: "telegram_connection_status",
      request_id: outbound.request_id,
      connection: {
        state: "setup_pending",
        enabled: true,
        bot_name: "Zen",
        bot_username: "zen_fixture_bot",
        binding_pending: false,
      },
    });
    await expect(pending).resolves.toEqual({
      state: "setup_pending",
      enabled: true,
      bot_name: "Zen",
      bot_username: "zen_fixture_bot",
      binding_pending: false,
    });
    expect(registeredHandlerCount(client)).toBe(0);
    client.disconnectAll();
  });

  test("binding challenge is validated and scoped to the current daemon", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client, server);
    socket.open();

    const pending = client.beginTelegramBinding(server.id);
    const outbound = JSON.parse(socket.sent.at(-1)!);
    expect(outbound.type).toBe("telegram_connection_bind");
    socket.receive({
      type: "telegram_binding_challenge",
      request_id: outbound.request_id,
      challenge: {
        url: "https://t.me/zen_fixture_bot?start=fixture",
        expires_at: "2026-08-24T12:10:00Z",
      },
    });
    await expect(pending).resolves.toEqual({
      url: "https://t.me/zen_fixture_bot?start=fixture",
      expires_at: "2026-08-24T12:10:00Z",
    });
    expect(registeredHandlerCount(client)).toBe(0);
    client.disconnectAll();
  });
});
