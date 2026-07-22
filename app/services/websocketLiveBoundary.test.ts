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

  test("Skills search cancellation is small and generation-correlated", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.searchSkillsCatalog(server.id, {
      query: "react",
      generation: 7,
    });
    const searchFrame = JSON.parse(socket.sent[0]!);
    expect(client.cancelSkillsCatalogSearch(server.id, { generation: 7 })).toBe(
      true,
    );
    expect(JSON.parse(socket.sent[1]!)).toMatchObject({
      type: "skills_search_cancel",
      generation: 7,
    });
    socket.receive({
      type: "skills_search_error",
      request_id: searchFrame.request_id,
      generation: 7,
      code: "canceled",
      message: "The Skills search was canceled.",
    });
    await expect(pending).rejects.toThrow("canceled");
    client.disconnectAll();
  });

  test("Skills rankings use one current-server frame and reject a stale generation", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.getSkillsLeaderboards(server.id, {
      generation: 6,
      limit: 30,
    });
    const outbound = JSON.parse(socket.sent.at(-1)!);
    expect(outbound).toMatchObject({
      type: "skills_catalog",
      generation: 6,
      limit: 30,
    });
    socket.receive({
      type: "skills_catalog",
      request_id: outbound.request_id,
      generation: 5,
      leaderboards: {},
    });

    await expect(pending).rejects.toThrow("stale Skills catalog generation");
    client.disconnectAll();
  });

  test("Skills install response rejects a same-name different repository and old unbound daemon", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();
    const options = {
      operation: "install" as const,
      skillId: "acme/skills/useful",
      source: "acme/skills",
      skillName: "useful",
      scope: "global" as const,
      agents: ["codex" as const],
    };

    const mismatched = client.buildSkillsCommand(server.id, options);
    const mismatchedRequest = JSON.parse(socket.sent.at(-1)!);
    socket.receive({
      type: "skills_command",
      request_id: mismatchedRequest.request_id,
      command: {
        operation: "install",
        command:
          "npx skills add https://github.com/other/skills --skill useful --global --agent codex --yes",
        catalog_id: "other/skills/useful",
        source: "other/skills",
        skill_name: "useful",
        scope: "global",
        agents: ["codex"],
      },
    });
    await expect(mismatched).rejects.toThrow("different request");

    const unbound = client.buildSkillsCommand(server.id, options);
    const unboundRequest = JSON.parse(socket.sent.at(-1)!);
    socket.receive({
      type: "skills_command",
      request_id: unboundRequest.request_id,
      command: {
        operation: "install",
        command:
          "npx skills add https://github.com/acme/skills --skill useful --global --agent codex --yes",
        skill_name: "useful",
        scope: "global",
        agents: ["codex"],
      },
    });
    await expect(unbound).rejects.toThrow("unbound Skills install command");
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
        warnings: [],
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
      },
    });
    client.disconnectAll();
  });

  test("a mismatched search generation is rejected without accepting results", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.searchSkillsCatalog(server.id, {
      query: "react native",
      limit: 20,
      generation: 8,
    });
    const outbound = JSON.parse(socket.sent.at(-1)!);
    expect(outbound).toMatchObject({
      type: "skills_search",
      prompt: "react native",
      limit: 20,
      generation: 8,
    });

    socket.receive({
      type: "skills_search",
      request_id: outbound.request_id,
      generation: 7,
      result: { query: "react native", skills: [] },
    });

    await expect(pending).rejects.toThrow("stale Skills search generation");
    client.disconnectAll();
  });

  test("command construction sends structured fields and accepts one exact official command", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.buildSkillsCommand(server.id, {
      operation: "install",
      skillId: "acme/skills/useful",
      source: "acme/skills",
      skillName: "useful",
      scope: "global",
      agents: ["codex", "cursor"],
    });
    const outbound = JSON.parse(socket.sent.at(-1)!);
    expect(outbound).toMatchObject({
      type: "skills_command",
      operation: "install",
      skill_id: "acme/skills/useful",
      source: "acme/skills",
      skill_name: "useful",
      scope: "global",
      agents: ["codex", "cursor"],
    });

    const command =
      "npx skills add https://github.com/acme/skills --skill useful --global --agent codex --agent cursor --yes";
    socket.receive({
      type: "skills_command",
      request_id: outbound.request_id,
      command: {
        operation: "install",
        command,
        catalog_id: "acme/skills/useful",
        source: "acme/skills",
        skill_name: "useful",
        scope: "global",
        agents: ["codex", "cursor"],
      },
    });

    await expect(pending).resolves.toEqual({
      operation: "install",
      command,
      catalogId: "acme/skills/useful",
      source: "acme/skills",
      skillName: "useful",
      scope: "global",
      agents: ["codex", "cursor"],
    });
    client.disconnectAll();
  });

  test("a valid command for different structured targets is rejected", async () => {
    const client = new MultiServerWebSocketClient();
    const socket = await connectClient(client);
    socket.open();

    const pending = client.buildSkillsCommand(server.id, {
      operation: "remove",
      skillId: "0123456789abcdef01234567",
      skillName: "useful",
      scope: "global",
      agents: ["codex"],
    });
    const outbound = JSON.parse(socket.sent.at(-1)!);
    socket.receive({
      type: "skills_command",
      request_id: outbound.request_id,
      command: {
        operation: "remove",
        command: "npx skills remove other-skill --global --agent codex --yes",
        skill_name: "other-skill",
        scope: "global",
        agents: ["codex"],
      },
    });

    await expect(pending).rejects.toThrow(
      "Skills command for a different request",
    );
    client.disconnectAll();
  });
});
