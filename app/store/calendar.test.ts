import { describe, expect, test } from "bun:test";
import {
  calendarReducer,
  initialCalendarState,
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
