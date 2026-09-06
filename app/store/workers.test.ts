// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  workerReducer,
  countWorkersByServer,
  initialWorkerState,
  reconcileServerWorkers,
  type Worker,
} from "./workers";

function agent(id: string, overrides: Partial<Worker> = {}): Worker {
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

describe("reconcileServerWorkers", () => {
  test("keeps known IDs stable across interleaved activity updates", () => {
    const current = [agent("a"), agent("b"), agent("c")];
    const incoming = [
      agent("c", { status: "running", updated_at: 30 }),
      agent("a", { summary: "heartbeat", updated_at: 40 }),
      agent("b", { last_output_lines: ["transcript"], updated_at: 20 }),
    ];

    const next = reconcileServerWorkers(current, "server", incoming);
    expect(next.map(item => item.id)).toEqual(["a", "b", "c"]);
    expect(next.map(item => item.updated_at)).toEqual([40, 20, 30]);
  });

  test("reconnect snapshots remove missing IDs and append new IDs deterministically", () => {
    const other = agent("remote", { key: "other:remote", serverId: "other" });
    const current = [agent("a"), other, agent("b"), agent("removed")];
    const incoming = [agent("new-2"), agent("b"), agent("new-1"), agent("a")];

    const next = reconcileServerWorkers(current, "server", incoming);
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
    let state = workerReducer(initialWorkerState, {
      type: "UPSERT_SERVER_WORKERS",
      serverId: "server",
      serverName: "Server",
      serverUrl: "https://server.test",
      workers: [agent("a"), agent("b")],
    });
    expect(countWorkersByServer(state.workers)).toEqual({ server: 2 });

    state = workerReducer(state, {
      type: "UPSERT_WORKER",
      serverId: "server",
      serverName: "Server",
      serverUrl: "https://server.test",
      worker: agent("c"),
    });
    expect(countWorkersByServer(state.workers)).toEqual({ server: 3 });

    state = workerReducer(state, {
      type: "REMOVE_WORKER",
      serverId: "server",
      worker_id: "b",
    });
    expect(countWorkersByServer(state.workers)).toEqual({ server: 2 });

    state = workerReducer(state, {
      type: "UPSERT_SERVER_WORKERS",
      serverId: "empty",
      serverName: "Empty",
      serverUrl: "https://empty.test",
      workers: [],
    });
    expect(state.hydratedServers.empty).toBe(true);
    expect(countWorkersByServer(state.workers)).toEqual({ server: 2 });

    state = workerReducer(state, { type: "REMOVE_SERVER", serverId: "server" });
    expect(countWorkersByServer(state.workers)).toEqual({});
    expect(state.hydratedServers.empty).toBe(true);

    state = workerReducer(state, { type: "REMOVE_SERVER", serverId: "empty" });
    expect(state.hydratedServers.empty).toBeUndefined();
  });
});

describe("agent timestamp normalization", () => {
  test("invalid or missing updated_at becomes undefined, never the device clock", () => {
    const payload = {
      type: "UPSERT_SERVER_WORKERS" as const,
      serverId: "server",
      serverName: "Server",
      serverUrl: "https://server.test",
      workers: [
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

    const state = workerReducer(initialWorkerState, payload);
    for (const next of state.workers) {
      expect(next.updated_at).toBeUndefined();
    }
  });

  test("valid timestamps preserve seconds and milliseconds forms", () => {
    const millis = 1_752_960_000_000;
    const state = workerReducer(initialWorkerState, {
      type: "UPSERT_SERVER_WORKERS",
      serverId: "server",
      serverName: "Server",
      serverUrl: "https://server.test",
      workers: [
        { id: "seconds", name: "seconds", status: "unknown", summary: "", updated_at: Math.floor(millis / 1000) },
        { id: "millis", name: "millis", status: "unknown", summary: "", updated_at: millis },
        { id: "iso", name: "iso", status: "unknown", summary: "", updated_at: new Date(millis).toISOString() },
      ],
    });
    const byId = Object.fromEntries(state.workers.map(item => [item.id, item]));
    expect(byId.seconds.updated_at).toBe(millis);
    expect(byId.millis.updated_at).toBe(millis);
    expect(byId.iso.updated_at).toBe(millis);
  });
});

describe("worker list ordering", () => {
  test("repeated no-op snapshots preserve timestamps and ordering exactly", () => {
    const payload = {
      type: "UPSERT_SERVER_WORKERS" as const,
      serverId: "server",
      serverName: "Server",
      serverUrl: "https://server.test",
      workers: [
        agent("c", { updated_at: 30 }),
        agent("a", { updated_at: 40 }),
        agent("b", { updated_at: 20 }),
      ],
    };

    const first = workerReducer(initialWorkerState, payload);
    const second = workerReducer(first, payload);
    expect(second).toBe(first);
    expect(second.workers.map(item => item.id)).toEqual(["c", "a", "b"]);
    expect(second.workers.map(item => item.updated_at)).toEqual([30000, 40000, 20000]);
  });

  test("ordering changes only when the server reports newer activity", () => {
    const base = {
      type: "UPSERT_SERVER_WORKERS" as const,
      serverId: "server",
      serverName: "Server",
      serverUrl: "https://server.test",
      workers: [
        agent("c", { updated_at: 30 }),
        agent("a", { updated_at: 40 }),
        agent("b", { updated_at: 20 }),
      ],
    };
    let state = workerReducer(initialWorkerState, base);

    // No-op refresh must not reorder.
    state = workerReducer(state, base);
    expect(state.workers.map(item => item.id)).toEqual(["c", "a", "b"]);

    // Newer meaningful activity updates that row in place; the App preserves
    // the server-provided ordering (the daemon reorders by activity time).
    state = workerReducer(state, {
      ...base,
      workers: [
        agent("c", { updated_at: 30 }),
        agent("a", { updated_at: 40 }),
        agent("b", { status: "done", updated_at: 50 }),
      ],
    });
    expect(state.workers.map(item => item.id)).toEqual(["c", "a", "b"]);
    expect(state.workers.map(item => item.updated_at)).toEqual([30000, 40000, 50000]);
    expect(state.workers[2].status).toBe("done");
  });
});
