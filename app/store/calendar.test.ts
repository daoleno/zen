import { describe, expect, test } from "bun:test";
import {
  calendarReducer,
  initialCalendarState,
  selectCurrentServerCalendar,
  selectCurrentServerCalendarItems,
  type CalendarItem,
} from "./calendar";
const item = (id: string, next_at: string): CalendarItem => ({
  id,
  title: id,
  kind: "reminder",
  status: "scheduled",
  notify_at: next_at,
  timezone: "UTC",
  recurrence: "none",
  next_at,
  created_at: next_at,
  updated_at: next_at,
  revision: 1,
});
describe("calendarReducer", () => {
  test("hydrates sorted snapshots and upserts revisions", () => {
    const first = calendarReducer(initialCalendarState, {
      type: "CALENDAR_SNAPSHOT",
      serverId: "s",
      serverName: "Zen",
      serverUrl: "ws://z",
      items: [
        item("later", "2026-07-15T00:00:00Z"),
        item("now", "2026-07-14T00:00:00Z"),
      ],
    });
    expect(first.byServer.s.items.map((i) => i.id)).toEqual(["now", "later"]);
    const changed = { ...item("now", "2026-07-16T00:00:00Z"), revision: 2 };
    const second = calendarReducer(first, {
      type: "CALENDAR_CHANGED",
      serverId: "s",
      serverName: "Zen",
      serverUrl: "ws://z",
      item: changed,
    });
    expect(second.byServer.s.items.map((i) => i.id)).toEqual(["later", "now"]);
    expect(second.byServer.s.items[1].revision).toBe(2);
  });
  test("removes one server without touching another", () => {
    let state = calendarReducer(initialCalendarState, {
      type: "CALENDAR_SNAPSHOT",
      serverId: "a",
      serverName: "A",
      serverUrl: "",
      items: [],
    });
    state = calendarReducer(state, {
      type: "CALENDAR_SNAPSHOT",
      serverId: "b",
      serverName: "B",
      serverUrl: "",
      items: [],
    });
    state = calendarReducer(state, { type: "REMOVE_SERVER", serverId: "a" });
    expect(Object.keys(state.byServer)).toEqual(["b"]);
  });
});

describe("current-server Calendar projection", () => {
  test("exposes only the canonical current server and rebinds exactly", () => {
    let state = calendarReducer(initialCalendarState, {
      type: "CALENDAR_SNAPSHOT",
      serverId: "old",
      serverName: "Old Mac",
      serverUrl: "ws://old",
      items: [item("old-item", "2026-07-14T00:00:00Z")],
    });
    state = calendarReducer(state, {
      type: "CALENDAR_SNAPSHOT",
      serverId: "current",
      serverName: "Current Mac",
      serverUrl: "ws://current",
      items: [item("current-item", "2026-07-15T00:00:00Z")],
    });

    expect(selectCurrentServerCalendar(state, "current")?.serverName).toBe(
      "Current Mac",
    );
    expect(selectCurrentServerCalendarItems(state, "current")).toEqual([
      expect.objectContaining({
        id: "current-item",
        serverId: "current",
        serverName: "Current Mac",
      }),
    ]);
    expect(selectCurrentServerCalendarItems(state, "old")).toEqual([
      expect.objectContaining({ id: "old-item", serverId: "old" }),
    ]);
  });

  test("never invents a fallback server", () => {
    const state = calendarReducer(initialCalendarState, {
      type: "CALENDAR_SNAPSHOT",
      serverId: "only-configured",
      serverName: "Configured Mac",
      serverUrl: "ws://configured",
      items: [item("configured-item", "2026-07-14T00:00:00Z")],
    });

    expect(selectCurrentServerCalendar(state, null)).toBeNull();
    expect(selectCurrentServerCalendarItems(state, "missing")).toEqual([]);
  });
});
