import { describe, expect, test } from "bun:test";
import {
  createSessionTerminationEntries,
  SessionTerminationBatch,
  sessionTerminationConfirmMessage,
  sessionTerminationSummaryMessage,
  summarizeSessionTermination,
  SESSION_TERMINATION_OFFLINE_MESSAGE,
  SESSION_TERMINATION_TIMEOUT_MESSAGE,
  type SessionTerminationEntry,
  type SessionTerminationSummary,
  type SessionTerminationTransport,
} from "./sessionBulkTerminate";

interface KillRecord {
  serverId: string;
  agentId: string;
  requestId?: string;
}

class FakeTransport implements SessionTerminationTransport {
  kills: KillRecord[] = [];
  readonly listeners = new Map<string, Set<(data: any) => void>>();

  on(type: string, handler: (data: any) => void): void {
    const set = this.listeners.get(type) ?? new Set();
    set.add(handler);
    this.listeners.set(type, set);
  }

  off(type: string, handler: (data: any) => void): void {
    this.listeners.get(type)?.delete(handler);
  }

  killAgent(serverId: string, agentId: string, requestId?: string): void {
    this.kills.push({ serverId, agentId, requestId });
  }

  emit(type: string, data: any): void {
    for (const handler of this.listeners.get(type) ?? []) {
      handler(data);
    }
  }

  listenerCount(type: string): number {
    return this.listeners.get(type)?.size ?? 0;
  }
}

const targets = [
  { sessionKey: "[\"srv-a\",\"agent-1\"]", serverId: "srv-a", agentId: "agent-1" },
  { sessionKey: "[\"srv-a\",\"agent-2\"]", serverId: "srv-a", agentId: "agent-2" },
  { sessionKey: "[\"srv-b\",\"agent-3\"]", serverId: "srv-b", agentId: "agent-3" },
];

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

function settleAll(
  batch: SessionTerminationBatch,
  transport: FakeTransport,
): void {
  // Fail agent-2, succeed the rest via archived events.
  for (const kill of transport.kills) {
    if (kill.agentId === "agent-2") {
      transport.emit("error", {
        serverId: kill.serverId,
        request_id: kill.requestId,
        code: "close_failed",
        message: "session still live",
      });
    } else {
      transport.emit("agent_session_archived", {
        serverId: kill.serverId,
        agent_session: { id: kill.agentId },
      });
    }
  }
}

describe("createSessionTerminationEntries", () => {
  test("captures stable IDs only and starts fully pending", () => {
    const entries = createSessionTerminationEntries(targets);
    expect(entries).toEqual(
      targets.map((target) => ({ ...target, status: "pending" })),
    );
  });
});

describe("summarizeSessionTermination", () => {
  test("counts outcomes and reports failures in submission order", () => {
    const entries: SessionTerminationEntry[] = [
      { ...targets[0], status: "succeeded" },
      { ...targets[1], status: "failed", error: "boom" },
      { ...targets[2], status: "pending" },
    ];
    const summary = summarizeSessionTermination(entries);
    expect(summary).toEqual({
      total: 3,
      succeeded: 1,
      failed: 1,
      pending: 1,
      running: true,
      failedEntries: [entries[1]],
    });
  });
});

describe("SessionTerminationBatch.start", () => {
  test("submits every entry exactly once with a request_id", () => {
    const transport = new FakeTransport();
    const settled: SessionTerminationSummary[] = [];
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries(targets),
      onSettled: (summary) => settled.push(summary),
    });
    expect(batch.start()).toBe(true);
    expect(transport.kills).toHaveLength(3);
    for (const kill of transport.kills) {
      expect(kill.requestId).toBeTruthy();
    }
    expect(new Set(transport.kills.map((k) => k.agentId))).toEqual(
      new Set(["agent-1", "agent-2", "agent-3"]),
    );
  });

  test("second start returns false and never resubmits (duplicate prevention)", () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries(targets),
      onSettled: () => undefined,
    });
    expect(batch.start()).toBe(true);
    const killCount = transport.kills.length;
    expect(batch.start()).toBe(false);
    expect(transport.kills).toHaveLength(killCount);
  });

  test("offline transport failure settles immediately as retryable", () => {
    const transport = new FakeTransport();
    transport.killAgent = () => {
      throw new Error("Daemon is not connected.");
    };
    const settled: SessionTerminationSummary[] = [];
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0]]),
      onSettled: (summary) => settled.push(summary),
    });
    batch.start();
    expect(batch.summary).toMatchObject({
      total: 1,
      succeeded: 0,
      failed: 1,
      pending: 0,
      running: false,
    });
    expect(batch.summary.failedEntries[0].error).toBe(
      SESSION_TERMINATION_OFFLINE_MESSAGE,
    );
    expect(settled.at(-1)?.running).toBe(false);
  });
});

describe("SessionTerminationBatch settlement", () => {
  test("error reply with matching request_id fails that entry only", () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0], targets[1]]),
      onSettled: () => undefined,
    });
    batch.start();
    const kill = transport.kills.find((k) => k.agentId === "agent-1")!;
    transport.emit("error", {
      serverId: "srv-a",
      request_id: kill.requestId,
      message: "session still live",
    });
    const summary = batch.summary;
    expect(summary.failed).toBe(1);
    expect(summary.succeeded).toBe(0);
    expect(summary.failedEntries[0].error).toBe("session still live");
    expect(summary.failedEntries[0].sessionKey).toBe(targets[0].sessionKey);
  });

  test("error reply for another request or server is ignored", () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0]]),
      onSettled: () => undefined,
    });
    batch.start();
    transport.emit("error", {
      serverId: "srv-a",
      request_id: "some-other-request",
      message: "unrelated",
    });
    transport.emit("error", {
      serverId: "srv-other",
      request_id: transport.kills[0].requestId,
      message: "unrelated server",
    });
    expect(batch.summary.pending).toBe(1);
    expect(batch.summary.failed).toBe(0);
  });

  test("agent_session_archived settles the matching Session as success", () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0]]),
      onSettled: () => undefined,
    });
    batch.start();
    transport.emit("agent_session_archived", {
      serverId: "srv-a",
      agent_session: { id: "agent-1" },
    });
    expect(batch.summary.succeeded).toBe(1);
    expect(batch.summary.running).toBe(false);
  });

  test("absence from a full agent_session_list settles as success", () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0], targets[2]]),
      onSettled: () => undefined,
    });
    batch.start();
    // Snapshot without agent-1 but with agent-3: only agent-1 settles.
    transport.emit("agent_session_list", {
      serverId: "srv-a",
      agent_sessions: [{ id: "agent-9" }],
    });
    expect(batch.summary.succeeded).toBe(1);
    expect(batch.summary.pending).toBe(1);
    // Next snapshot without agent-3 settles the rest.
    transport.emit("agent_session_list", {
      serverId: "srv-b",
      agent_sessions: [],
    });
    expect(batch.summary.succeeded).toBe(2);
    expect(batch.summary.running).toBe(false);
  });

  test("presence in agent_session_list keeps the entry pending", () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0]]),
      onSettled: () => undefined,
    });
    batch.start();
    transport.emit("agent_session_list", {
      serverId: "srv-a",
      agent_sessions: [{ id: "agent-1" }],
    });
    expect(batch.summary.pending).toBe(1);
  });

  test("settlement is first-wins: archived then late error keeps success", () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0]]),
      onSettled: () => undefined,
    });
    batch.start();
    const kill = transport.kills[0];
    transport.emit("agent_session_archived", {
      serverId: "srv-a",
      agent_session: { id: "agent-1" },
    });
    transport.emit("error", {
      serverId: "srv-a",
      request_id: kill.requestId,
      message: "late failure",
    });
    expect(batch.summary.succeeded).toBe(1);
    expect(batch.summary.failed).toBe(0);
  });

  test("settleDisappeared treats authoritative disappearance as success", () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0]]),
      onSettled: () => undefined,
    });
    batch.start();
    batch.settleDisappeared(targets[0].sessionKey);
    expect(batch.summary.succeeded).toBe(1);
    expect(batch.summary.running).toBe(false);
  });

  test("timeout without any acknowledgement fails as retryable", async () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0]]),
      onSettled: () => undefined,
      timeoutMs: 10,
    });
    batch.start();
    expect(batch.summary.pending).toBe(1);
    await sleep(40);
    expect(batch.summary.failed).toBe(1);
    expect(batch.summary.failedEntries[0].error).toBe(
      SESSION_TERMINATION_TIMEOUT_MESSAGE,
    );
    expect(batch.summary.running).toBe(false);
  });

  test("partial failure: successes settle, failures reported accurately", () => {
    const transport = new FakeTransport();
    const finalSummaries: SessionTerminationSummary[] = [];
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries(targets),
      onSettled: (summary) => {
        if (summary.pending === 0) finalSummaries.push(summary);
      },
    });
    batch.start();
    settleAll(batch, transport);
    expect(finalSummaries).toHaveLength(1);
    const final = finalSummaries[0];
    expect(final.total).toBe(3);
    expect(final.succeeded).toBe(2);
    expect(final.failed).toBe(1);
    expect(final.failedEntries.map((e) => e.agentId)).toEqual(["agent-2"]);
    expect(final.running).toBe(false);
    // Never reports all succeeded when only some did.
    expect(final.succeeded).not.toBe(final.total);
  });

  test("all success reports a single settled summary", () => {
    const transport = new FakeTransport();
    const summaries: SessionTerminationSummary[] = [];
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0], targets[1]]),
      onSettled: (summary) => summaries.push(summary),
    });
    batch.start();
    for (const kill of transport.kills) {
      transport.emit("agent_session_archived", {
        serverId: kill.serverId,
        agent_session: { id: kill.agentId },
      });
    }
    expect(batch.summary.succeeded).toBe(2);
    expect(batch.summary.failed).toBe(0);
    const finals = summaries.filter((summary) => !summary.running);
    expect(finals).toHaveLength(1);
    expect(finals[0].succeeded).toBe(2);
  });
});

describe("SessionTerminationBatch dispose", () => {
  test("removes listeners and stops timers", async () => {
    const transport = new FakeTransport();
    const summaries: SessionTerminationSummary[] = [];
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0]]),
      onSettled: (summary) => summaries.push(summary),
      timeoutMs: 10,
    });
    batch.start();
    expect(transport.listenerCount("error")).toBe(1);
    expect(transport.listenerCount("agent_session_archived")).toBe(1);
    expect(transport.listenerCount("agent_session_list")).toBe(1);
    expect(summaries).toHaveLength(1); // initial running summary from start()
    batch.dispose();
    expect(transport.listenerCount("error")).toBe(0);
    expect(transport.listenerCount("agent_session_archived")).toBe(0);
    expect(transport.listenerCount("agent_session_list")).toBe(0);
    // No event or timer may settle after dispose.
    transport.emit("agent_session_archived", {
      serverId: "srv-a",
      agent_session: { id: "agent-1" },
    });
    await sleep(40);
    expect(summaries).toHaveLength(1);
    expect(batch.summary.pending).toBe(1);
  });
});

describe("confirmation and summary copy", () => {
  test("confirmation names the exact selected count", () => {
    expect(sessionTerminationConfirmMessage(1, 1)).toContain(
      "This session will be terminated.",
    );
    expect(sessionTerminationConfirmMessage(3, 1)).toContain(
      "These 3 sessions will be terminated.",
    );
    expect(sessionTerminationConfirmMessage(3, 2)).toContain(
      "across 2 daemons",
    );
  });

  test("partial failure message never claims full success", () => {
    const entries: SessionTerminationEntry[] = [
      { ...targets[0], status: "succeeded" },
      { ...targets[1], status: "failed", error: "boom" },
    ];
    const summary = summarizeSessionTermination(entries);
    const message = sessionTerminationSummaryMessage(summary, ["agent-2"]);
    expect(message).toContain("1 of 2 terminated");
    expect(message).toContain("1 session failed");
    expect(message).toContain("agent-2");
    expect(message).toContain("remain selected");
    expect(message).not.toContain("all");
  });

  test("total failure message names the count and retry", () => {
    const entries: SessionTerminationEntry[] = [
      { ...targets[0], status: "failed", error: "boom" },
    ];
    const summary = summarizeSessionTermination(entries);
    const message = sessionTerminationSummaryMessage(summary, []);
    expect(message).toContain("Could not terminate 1 session");
    expect(message).toContain("Retry to terminate");
  });

  test("empty summary yields no failure message", () => {
    const entries: SessionTerminationEntry[] = [
      { ...targets[0], status: "succeeded" },
    ];
    expect(sessionTerminationSummaryMessage(summarizeSessionTermination(entries), [])).toBe("");
  });
});
