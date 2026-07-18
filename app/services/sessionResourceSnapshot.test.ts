// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  acceptSessionResourceSnapshotResponse,
  formatByteSize,
  formatUptime,
  normalizeSessionResourceSnapshot,
} from "./sessionResourceSnapshot";

describe("sessionResourceSnapshot", () => {
  test("normalizes optional measurements and omits absent values", () => {
    const snapshot = normalizeSessionResourceSnapshot({
      request_id: "req-1",
      agent_id: "main:@7",
      generation: "ignored-if-present",
      session: {
        name: "cursor-agent",
        executor: "cursor",
        status: "running",
        managed: true,
        backend: "cgroup_pool",
        memory_current_bytes: 1500,
        tasks_current: 3,
      },
      pool: {
        backend: "cgroup_pool",
        memory_high_bytes: 25000000000,
        memory_max_bytes: 28000000000,
      },
      host: {
        available_bytes: 8589934592,
        pressure: "ok",
      },
    });

    expect(snapshot).toEqual({
      request_id: "req-1",
      agent_id: "main:@7",
      session: {
        name: "cursor-agent",
        executor: "cursor",
        status: "running",
        managed: true,
        backend: "cgroup_pool",
        memory_current_bytes: 1500,
        tasks_current: 3,
      },
      pool: {
        backend: "cgroup_pool",
        memory_high_bytes: 25000000000,
        memory_max_bytes: 28000000000,
      },
      host: {
        available_bytes: 8589934592,
        pressure: "ok",
      },
    });
  });

  test("rejects snapshots missing agent identity", () => {
    expect(
      normalizeSessionResourceSnapshot({
        request_id: "req",
        session: {},
      }),
    ).toBeNull();
  });

  test("request-epoch invalidation rejects stale or retargeted responses", () => {
    expect(
      acceptSessionResourceSnapshotResponse({
        requestSeq: 3,
        currentSeq: 3,
        snapshotAgentId: "main:@7",
        expectedAgentId: "main:@7",
      }),
    ).toBe(true);
    expect(
      acceptSessionResourceSnapshotResponse({
        requestSeq: 3,
        currentSeq: 4,
        snapshotAgentId: "main:@7",
        expectedAgentId: "main:@7",
      }),
    ).toBe(false);
    expect(
      acceptSessionResourceSnapshotResponse({
        requestSeq: 3,
        currentSeq: 3,
        snapshotAgentId: "main:@8",
        expectedAgentId: "main:@7",
      }),
    ).toBe(false);
    expect(
      acceptSessionResourceSnapshotResponse({
        requestSeq: 1,
        currentSeq: 1,
        snapshotAgentId: "main:@7",
        expectedAgentId: "",
      }),
    ).toBe(false);
  });

  test("close/reconnect/session change bumps epoch and drops in-flight accepts", () => {
    let epoch = 0;
    const bump = () => {
      epoch += 1;
      return epoch;
    };

    const openSeq = bump(); // open/load
    expect(
      acceptSessionResourceSnapshotResponse({
        requestSeq: openSeq,
        currentSeq: epoch,
        snapshotAgentId: "main:@7",
        expectedAgentId: "main:@7",
      }),
    ).toBe(true);

    const afterClose = bump(); // close / disconnect / session change
    expect(
      acceptSessionResourceSnapshotResponse({
        requestSeq: openSeq,
        currentSeq: afterClose,
        snapshotAgentId: "main:@7",
        expectedAgentId: "main:@7",
      }),
    ).toBe(false);

    const nextOpen = bump();
    expect(
      acceptSessionResourceSnapshotResponse({
        requestSeq: nextOpen,
        currentSeq: epoch,
        snapshotAgentId: "main:@9",
        expectedAgentId: "main:@9",
      }),
    ).toBe(true);
  });

  test("formats human-readable sizes and uptime", () => {
    expect(formatByteSize(1536)).toBe("1.50 KiB");
    expect(formatByteSize(10 * 1024 * 1024 * 1024)).toBe("10.0 GiB");
    expect(
      formatUptime("2026-07-18T12:00:00.000Z", Date.parse("2026-07-18T13:05:04.000Z")),
    ).toBe("1h 5m");
  });
});
