// @ts-nocheck
import { expect, test } from "bun:test";
import type { Agent } from "../store/agents";
import { sortTerminalAgents } from "../app/terminal/TerminalScreenModel";

function agent(id: string, updatedAt: number, status: Agent["status"]): Agent {
  return {
    key: `server:${id}`,
    id,
    serverId: "server",
    serverName: "Server",
    serverUrl: "https://server.test",
    name: id,
    status,
    summary: "",
    last_output_lines: [],
    updated_at: updatedAt,
  };
}

test("terminal session order ignores recent opens, status, and activity time", () => {
  const agents = [agent("a", 1, "done"), agent("b", 100, "failed")];
  const result = sortTerminalAgents({
    agents,
    recentAgentOpens: { "server:b": 999 },
  });

  expect(result.map(item => item.id)).toEqual(["a", "b"]);
  expect(result).not.toBe(agents);
});
