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

beforeEach(() => {
  FakeWebSocket.instances = [];
});

afterAll(() => {
  Object.assign(globalThis, { WebSocket: originalWebSocket });
});

describe("generic WebSocket live boundary", () => {
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
    expect(() => client.sendInput(server.id, "agent-a", "offline"))
      .toThrow("Daemon is not connected.");
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
    expect(() => client.sendTerminalInput(server.id, "terminal-a", "pwd\n"))
      .toThrow("Daemon is not connected.");

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

    const actionReceipt = client.sendAction(server.id, "agent-a", "pause");
    expect(socket.sent).toHaveLength(2);
    expect(JSON.parse(socket.sent[1]!)).toEqual({
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
