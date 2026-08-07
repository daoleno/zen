// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  agentReducer,
  countAgentsByServer,
  initialAgentState,
  reconcileServerAgents,
  type Agent,
} from "./agents";

function agent(id: string, overrides: Partial<Agent> = {}): Agent {
  return {
    key: `server:${id}`,
    id,
    serverId: "server",
    serverName: "Server",
    serverUrl: "https://server.test",
    name: id,
    status: "unknown",
    summary: "",
    last_output_lines: [],
    updated_at: 1,
    ...overrides,
  };
}

describe("reconcileServerAgents", () => {
  test("keeps known IDs stable across interleaved activity updates", () => {
    const current = [agent("a"), agent("b"), agent("c")];
    const incoming = [
      agent("c", { status: "running", updated_at: 30 }),
      agent("a", { summary: "heartbeat", updated_at: 40 }),
      agent("b", { last_output_lines: ["transcript"], updated_at: 20 }),
    ];

    const next = reconcileServerAgents(current, "server", incoming);
    expect(next.map(item => item.id)).toEqual(["a", "b", "c"]);
    expect(next.map(item => item.updated_at)).toEqual([40, 20, 30]);
  });

  test("reconnect snapshots remove missing IDs and append new IDs deterministically", () => {
    const other = agent("remote", { key: "other:remote", serverId: "other" });
    const current = [agent("a"), other, agent("b"), agent("removed")];
    const incoming = [agent("new-2"), agent("b"), agent("new-1"), agent("a")];

    const next = reconcileServerAgents(current, "server", incoming);
    expect(next.map(item => item.key)).toEqual([
      "server:a",
      "other:remote",
      "server:b",
      "server:new-2",
      "server:new-1",
    ]);
  });
});

describe("authoritative agent counts", () => {
  test("derives counts through snapshots, upserts, removals, and server cleanup", () => {
    let state = agentReducer(initialAgentState, {
      type: "UPSERT_SERVER_AGENTS",
      serverId: "server",
      serverName: "Server",
      serverUrl: "https://server.test",
      agents: [agent("a"), agent("b")],
    });
    expect(countAgentsByServer(state.agents)).toEqual({ server: 2 });

    state = agentReducer(state, {
      type: "UPSERT_AGENT",
      serverId: "server",
      serverName: "Server",
      serverUrl: "https://server.test",
      agent: agent("c"),
    });
    expect(countAgentsByServer(state.agents)).toEqual({ server: 3 });

    state = agentReducer(state, {
      type: "REMOVE_AGENT",
      serverId: "server",
      agent_id: "b",
    });
    expect(countAgentsByServer(state.agents)).toEqual({ server: 2 });

    state = agentReducer(state, {
      type: "UPSERT_SERVER_AGENTS",
      serverId: "empty",
      serverName: "Empty",
      serverUrl: "https://empty.test",
      agents: [],
    });
    expect(state.hydratedServers.empty).toBe(true);
    expect(countAgentsByServer(state.agents)).toEqual({ server: 2 });

    state = agentReducer(state, { type: "REMOVE_SERVER", serverId: "server" });
    expect(countAgentsByServer(state.agents)).toEqual({});
    expect(state.hydratedServers.empty).toBe(true);

    state = agentReducer(state, { type: "REMOVE_SERVER", serverId: "empty" });
    expect(state.hydratedServers.empty).toBeUndefined();
  });
});

describe("agent timestamp normalization", () => {
  test("invalid or missing updated_at becomes undefined, never the device clock", () => {
    const payload = {
      type: "UPSERT_SERVER_AGENTS" as const,
      serverId: "server",
      serverName: "Server",
      serverUrl: "https://server.test",
      agents: [
        { id: "missing", name: "missing", status: "unknown" as const, summary: "" },
        {
          id: "garbage",
          name: "garbage",
          status: "unknown" as const,
          summary: "",
          updated_at: "not-a-date",
        },
        {
          id: "epoch-zero",
          name: "epoch-zero",
          status: "unknown" as const,
          summary: "",
          updated_at: 0,
        },
        {
          id: "epoch-string",
          name: "epoch-string",
          status: "unknown" as const,
          summary: "",
          updated_at: "0001-01-01T00:00:00Z",
        },
      ],
    };

    const state = agentReducer(initialAgentState, payload);
    for (const next of state.agents) {
      expect(next.updated_at).toBeUndefined();
    }
  });

  test("valid timestamps preserve seconds and milliseconds forms", () => {
    const millis = 1_752_960_000_000;
    const state = agentReducer(initialAgentState, {
      type: "UPSERT_SERVER_AGENTS",
      serverId: "server",
      serverName: "Server",
      serverUrl: "https://server.test",
      agents: [
        { id: "seconds", name: "seconds", status: "unknown", summary: "", updated_at: Math.floor(millis / 1000) },
        { id: "millis", name: "millis", status: "unknown", summary: "", updated_at: millis },
        { id: "iso", name: "iso", status: "unknown", summary: "", updated_at: new Date(millis).toISOString() },
      ],
    });
    const byId = Object.fromEntries(state.agents.map(item => [item.id, item]));
    expect(byId.seconds.updated_at).toBe(millis);
    expect(byId.millis.updated_at).toBe(millis);
    expect(byId.iso.updated_at).toBe(millis);
  });
});

describe("agent list ordering", () => {
  test("repeated no-op snapshots preserve timestamps and ordering exactly", () => {
    const payload = {
      type: "UPSERT_SERVER_AGENTS" as const,
      serverId: "server",
      serverName: "Server",
      serverUrl: "https://server.test",
      agents: [
        agent("c", { updated_at: 30 }),
        agent("a", { updated_at: 40 }),
        agent("b", { updated_at: 20 }),
      ],
    };

    const first = agentReducer(initialAgentState, payload);
    const second = agentReducer(first, payload);
    expect(second).toBe(first);
    expect(second.agents.map(item => item.id)).toEqual(["c", "a", "b"]);
    expect(second.agents.map(item => item.updated_at)).toEqual([30000, 40000, 20000]);
  });

  test("ordering changes only when the server reports newer activity", () => {
    const base = {
      type: "UPSERT_SERVER_AGENTS" as const,
      serverId: "server",
      serverName: "Server",
      serverUrl: "https://server.test",
      agents: [
        agent("c", { updated_at: 30 }),
        agent("a", { updated_at: 40 }),
        agent("b", { updated_at: 20 }),
      ],
    };
    let state = agentReducer(initialAgentState, base);

    // No-op refresh must not reorder.
    state = agentReducer(state, base);
    expect(state.agents.map(item => item.id)).toEqual(["c", "a", "b"]);

    // Newer meaningful activity updates that row in place; the App preserves
    // the server-provided ordering (the daemon reorders by activity time).
    state = agentReducer(state, {
      ...base,
      agents: [
        agent("c", { updated_at: 30 }),
        agent("a", { updated_at: 40 }),
        agent("b", { status: "done", updated_at: 50 }),
      ],
    });
    expect(state.agents.map(item => item.id)).toEqual(["c", "a", "b"]);
    expect(state.agents.map(item => item.updated_at)).toEqual([30000, 40000, 50000]);
    expect(state.agents[2].status).toBe("done");
  });
});
