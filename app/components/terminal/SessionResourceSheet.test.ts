// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { buildSessionResourceViewModel } from "./SessionResourceSheetModel";

const NOW = Date.parse("2026-07-18T13:05:04.000Z");

describe("SessionResourceSheet presentation model", () => {
  test("Linux split composition with live protection marker", () => {
    const model = buildSessionResourceViewModel(
      {
        agent_id: "main:@7",
        session: {
          executor: "cursor",
          status: "running",
          phase: "working",
          started_at: "2026-07-18T12:00:00.000Z",
          cwd: "/home/daoleno/workspace/zen",
          managed: true,
          backend: "cgroup_pool",
          memory_current_bytes: 2.4 * 1024 ** 3,
          memory_peak_bytes: 3 * 1024 ** 3,
          tasks_current: 4,
        },
        pool: {
          backend: "cgroup_pool",
          memory_current_bytes: 8 * 1024 ** 3,
          memory_high_bytes: 25 * 1024 ** 3,
          memory_max_bytes: 28 * 1024 ** 3,
        },
        host: { available_bytes: 10 * 1024 ** 3, pressure: "ok" },
      },
      NOW,
    );

    expect(model?.memoryLabel).toContain("GiB");
    expect(model?.tasksLabel).toBe("4 tasks");
    expect(model?.qualifier).toBe("Measured by the system");
    expect(model?.shareNote).toBeUndefined();
    expect(model?.bar?.split).toBe(true);
    expect(model?.bar?.session).toBeCloseTo(2.4 / 28, 5);
    expect(model?.bar?.other).toBeCloseTo(5.6 / 28, 5);
    expect(model?.bar?.protectionAt).toBeCloseTo(25 / 28, 5);
    expect(model?.otherLabel).toContain("Other Agents");
    expect(model?.hostChip).toBe("Enough memory headroom");
    expect(model?.hostNote).toBeUndefined();
    expect(JSON.stringify(model)).not.toMatch(/launch|definitely|looks sufficient/i);
    expect(model?.metaLine).toContain("1h 5m");
  });

  test("portable: policy hard max may bar; MemoryHigh is not live protection", () => {
    const noMax = buildSessionResourceViewModel({
      agent_id: "main:@7",
      session: {
        managed: true,
        backend: "portable_supervisor",
        memory_current_bytes: 1500,
        tasks_current: 2,
      },
      pool: { backend: "portable_supervisor", memory_current_bytes: 2000 },
    });
    expect(noMax?.qualifier).toBe("Estimated from owned processes");
    expect(noMax?.bar).toBeUndefined();
    expect(noMax?.poolSummary).toBe("1.95 KiB used");

    const withPolicy = buildSessionResourceViewModel({
      agent_id: "main:@7",
      session: {
        managed: true,
        backend: "portable_supervisor",
        memory_current_bytes: 1500,
      },
      pool: {
        backend: "portable_supervisor",
        memory_current_bytes: 2000,
        memory_high_bytes: 25 * 1024 ** 3,
        memory_max_bytes: 28 * 1024 ** 3,
      },
    });
    expect(withPolicy?.bar?.protectionAt).toBeUndefined();
    expect(withPolicy?.bar?.split).toBe(true);
    expect(withPolicy?.bar?.session).toBeGreaterThan(0);
  });

  test("over-limit clamps shares while keeping absolute summary labels", () => {
    const model = buildSessionResourceViewModel({
      agent_id: "main:@7",
      session: {
        managed: true,
        backend: "cgroup_pool",
        memory_current_bytes: 12,
      },
      pool: {
        backend: "cgroup_pool",
        memory_current_bytes: 15,
        memory_high_bytes: 8,
        memory_max_bytes: 10,
      },
    });
    expect(model?.memoryLabel).toBe("12 B");
    expect(model?.poolSummary).toBe("15 B used of 10 B");
    const bar = model?.bar;
    expect((bar?.session ?? 0) + (bar?.other ?? 0) + (bar?.remaining ?? 0)).toBeCloseTo(
      1,
      5,
    );
    expect(bar?.protectionAt).toBeCloseTo(0.8, 5);
  });

  test("session > pool: total-used bar, skew copy, no invented Other share", () => {
    const model = buildSessionResourceViewModel({
      agent_id: "main:@7",
      session: {
        managed: true,
        backend: "cgroup_pool",
        memory_current_bytes: 9000,
      },
      pool: {
        backend: "cgroup_pool",
        memory_current_bytes: 5000,
        memory_max_bytes: 20000,
      },
    });
    expect(model?.bar?.split).toBe(false);
    expect(model?.bar?.other).toBe(0);
    expect(model?.bar?.session).toBeCloseTo(5000 / 20000, 5);
    expect(model?.otherLabel).toBeUndefined();
    expect(model?.skewNote).toBe("Session and pool readings updated separately");
  });

  test("absent hard max keeps absolute labels without a fake percent bar", () => {
    const model = buildSessionResourceViewModel({
      agent_id: "main:@7",
      session: {
        managed: true,
        backend: "cgroup_pool",
        memory_current_bytes: 1000,
      },
      pool: { backend: "cgroup_pool", memory_current_bytes: 2000 },
    });
    expect(model?.poolSummary).toBe("1.95 KiB used");
    expect(model?.bar).toBeUndefined();
  });

  test("absent pool usage never turns Session memory into a pool bar", () => {
    const model = buildSessionResourceViewModel({
      agent_id: "main:@7",
      session: {
        managed: true,
        backend: "cgroup_pool",
        memory_current_bytes: 1000,
      },
      pool: { backend: "cgroup_pool", memory_max_bytes: 4000 },
    });
    expect(model?.poolSummary).toBe("Hard limit 3.91 KiB");
    expect(model?.bar).toBeUndefined();
    expect(model?.accessibilityLabel).toContain("1000 bytes");
  });

  test("unmanaged refuses stale pool and shows compact unmanaged state", () => {
    const model = buildSessionResourceViewModel({
      agent_id: "main:@8",
      session: {
        status: "running",
        managed: false,
        cwd: "/home/daoleno/workspace/zen",
      },
      pool: {
        backend: "cgroup_pool",
        memory_current_bytes: 8 * 1024 ** 3,
        memory_high_bytes: 25 * 1024 ** 3,
        memory_max_bytes: 28 * 1024 ** 3,
      },
      host: { available_bytes: 10 * 1024 ** 3, pressure: "ok" },
    });
    expect(model?.unmanagedNote).toBe("Not resource-managed by Zen");
    expect(model?.showSessionHero).toBe(false);
    expect(model?.memoryLabel).toBeNull();
    expect(model?.qualifier).toBeUndefined();
    expect(model?.poolSummary).toBeUndefined();
    expect(model?.bar).toBeUndefined();
    expect(model?.otherLabel).toBeUndefined();
    expect(model?.skewNote).toBeUndefined();
    expect(model?.hostChip).toBe("Enough memory headroom");
    expect(model?.hostAvailable).toContain("GiB");
    expect(model?.hostNote).toBeUndefined();
    expect(model?.metaLine).toContain("running");
    expect(model?.workspace).toBe("/home/daoleno/workspace/zen");
  });

  test("pressure wait copy is memory-headroom limited", () => {
    const model = buildSessionResourceViewModel({
      agent_id: "main:@7",
      session: { managed: true, backend: "cgroup_pool", memory_current_bytes: 1 },
      host: { available_bytes: 1024, pressure: "pressure" },
    });
    expect(model?.hostStatus).toBe("wait");
    expect(model?.hostNote).toBe("Agents may wait for memory headroom");
    expect(JSON.stringify(model)).not.toMatch(/kernel pressure|definitely launch/i);
  });
});
