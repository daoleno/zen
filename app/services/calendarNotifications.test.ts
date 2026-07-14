import { describe, expect, test } from "bun:test";
import { reconcileCalendarNotifications } from "./calendarNotificationReconciler";
import type { CalendarItem } from "../store/calendar";

const base: CalendarItem = {
  id: "reminder",
  title: "Item",
  kind: "reminder",
  status: "scheduled",
  notify_at: "2026-07-14T09:00:00Z",
  timezone: "UTC",
  recurrence: "daily",
  next_at: "2026-07-15T09:00:00Z",
  created_at: "2026-07-14T09:00:00Z",
  updated_at: "2026-07-14T09:00:00Z",
  revision: 1,
};

function harness(initial = "{}") {
  let stored = initial;
  const scheduled: any[] = [];
  const cancelled: string[] = [];
  let sequence = 0;
  return {
    scheduled,
    cancelled,
    value: () => JSON.parse(stored),
    deps: {
      getStored: async () => stored,
      setStored: async (value: string) => {
        stored = value;
      },
      schedule: async (request: any) => {
        scheduled.push(request);
        sequence += 1;
        return `notification-${sequence}`;
      },
      cancel: async (id: string) => {
        cancelled.push(id);
      },
      ensureChannel: async () => undefined,
    },
  };
}

describe("calendar notification reconciliation", () => {
  test("schedules the canonical next occurrence, not original notify_at", async () => {
    const mock = harness();
    await reconcileCalendarNotifications(
      "server",
      [base],
      mock.deps,
      Date.parse("2026-07-14T10:00:00Z"),
    );
    expect(mock.scheduled).toHaveLength(1);
    expect(mock.scheduled[0].trigger).toBe(base.next_at);
    expect(mock.value()["server:reminder"].trigger).toBe(base.next_at);
  });

  test("reuses only an exact trigger and content fingerprint", async () => {
    const mock = harness();
    const now = Date.parse("2026-07-14T10:00:00Z");
    await reconcileCalendarNotifications("server", [base], mock.deps, now);
    await reconcileCalendarNotifications("server", [base], mock.deps, now);
    expect(mock.scheduled).toHaveLength(1);
    expect(mock.cancelled).toEqual([]);

    await reconcileCalendarNotifications(
      "server",
      [{ ...base, title: "Changed", revision: 2 }],
      mock.deps,
      now,
    );
    expect(mock.cancelled).toEqual(["notification-1"]);
    expect(mock.scheduled).toHaveLength(2);
  });

  test("reschedules recurrence changes and cancels terminal or removed items", async () => {
    const mock = harness();
    const now = Date.parse("2026-07-14T10:00:00Z");
    await reconcileCalendarNotifications("server", [base], mock.deps, now);
    const next = { ...base, next_at: "2026-07-16T09:00:00Z", revision: 2 };
    await reconcileCalendarNotifications("server", [next], mock.deps, now);
    expect(mock.cancelled).toEqual(["notification-1"]);
    expect(mock.scheduled[1].trigger).toBe(next.next_at);

    await reconcileCalendarNotifications(
      "server",
      [{ ...next, status: "completed" }],
      mock.deps,
      now,
    );
    expect(mock.cancelled).toEqual(["notification-1", "notification-2"]);
    expect(mock.value()).toEqual({});
  });

  test("server removal does not cancel another server's notifications", async () => {
    const mock = harness();
    const now = Date.parse("2026-07-14T10:00:00Z");
    await reconcileCalendarNotifications("a", [base], mock.deps, now);
    await reconcileCalendarNotifications(
      "b",
      [{ ...base, id: "b" }],
      mock.deps,
      now,
    );
    await reconcileCalendarNotifications("a", [], mock.deps, now);
    expect(Object.keys(mock.value())).toEqual(["b:b"]);
    expect(mock.cancelled).toEqual(["notification-1"]);
  });
});
