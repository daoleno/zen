// @ts-nocheck
import { expect, test } from "bun:test";
import type { Worker } from "../store/workers";
import { sortTerminalWorkers } from "../components/terminal/screen/TerminalScreenModel";

function agent(id: string, updatedAt: number, status: Worker["status"]): Worker {
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
  const result = sortTerminalWorkers({
    agents,
    recentWorkerOpens: { "server:b": 999 },
  });

  expect(result.map(item => item.id)).toEqual(["a", "b"]);
  expect(result).not.toBe(agents);
});
