// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { reconcileServerAgents, type Agent } from "./agents";

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
