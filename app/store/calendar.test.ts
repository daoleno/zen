import { describe, expect, test } from "bun:test";
import {
  calendarReducer,
  initialCalendarState,
  selectCurrentServerCalendar,
  type CalendarItem,
  type CalendarState,
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
const snapshot = (
  state: CalendarState,
  serverId: string,
  items: CalendarItem[] = [],
) =>
  calendarReducer(state, {
    type: "CALENDAR_SNAPSHOT",
    serverId,
    serverName: serverId,
    serverUrl: `ws://${serverId}`,
    items,
  });
describe("calendarReducer", () => {
  test("hydrates sorted snapshots and upserts revisions", () => {
    const first = snapshot(initialCalendarState, "s", [
      item("later", "2026-07-15T00:00:00Z"),
      item("now", "2026-07-14T00:00:00Z"),
    ]);
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
    let state = snapshot(initialCalendarState, "a");
    state = snapshot(state, "b");
    state = calendarReducer(state, { type: "REMOVE_SERVER", serverId: "a" });
    expect(Object.keys(state.byServer)).toEqual(["b"]);
  });
});

describe("current-server Calendar projection", () => {
  test("exposes only the canonical server and never invents a fallback", () => {
    const currentItem = item("current-item", "2026-07-15T00:00:00Z");
    const state = snapshot(
      snapshot(initialCalendarState, "old", [
        item("old-item", "2026-07-14T00:00:00Z"),
      ]),
      "current",
      [currentItem],
    );
    expect(selectCurrentServerCalendar(state, "current")).toEqual({
      serverId: "current",
      serverName: "current",
      serverUrl: "ws://current",
      hydrated: true,
      items: [currentItem],
    });
    expect(selectCurrentServerCalendar(state, null)).toBeNull();
    expect(selectCurrentServerCalendar(state, "missing")).toBeNull();
  });
});
