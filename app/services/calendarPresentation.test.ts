import { describe, expect, test } from "bun:test";
import {
  agendaSection,
  calendarDateKey,
  executes,
  groupAgenda,
} from "./calendarPresentation";
import type { CalendarItem } from "../store/calendar";
const make = (
  id: string,
  at: string,
  kind: CalendarItem["kind"] = "reminder",
): CalendarItem => ({
  id,
  title: id,
  kind,
  status: "scheduled",
  notify_at: kind === "reminder" ? at : undefined,
  due_at: kind !== "reminder" ? at : undefined,
  timezone: "UTC",
  recurrence: "none",
  next_at: at,
  created_at: at,
  updated_at: at,
  revision: 1,
});
describe("calendar presentation", () => {
  test("groups agenda-first buckets", () => {
    const now = new Date("2026-07-14T10:00:00Z");
    expect(agendaSection(make("today", "2026-07-14T18:00:00Z"), now)).toBe(
      "Today",
    );
    expect(
      groupAgenda([make("tomorrow", "2026-07-15T10:00:00Z")], now)[0].title,
    ).toBe("Tomorrow");
  });
  test("groups by calendar date in the explicit viewer timezone", () => {
    const now = new Date("2026-03-08T04:30:00Z"); // Mar 7, 23:30 in New York.
    const afterMidnight = make("tomorrow", "2026-03-08T05:30:00Z");
    expect(agendaSection(afterMidnight, now, "America/New_York")).toBe(
      "Tomorrow",
    );
    expect(agendaSection(afterMidnight, now, "UTC")).toBe("Today");
  });
  test("keeps calendar-week boundaries across the spring DST change", () => {
    const now = new Date("2026-03-07T17:00:00Z");
    const monday = make("monday", "2026-03-09T16:00:00Z");
    expect(agendaSection(monday, now, "America/New_York")).toBe("Later");
  });
  test("uses the viewer date even when the item timezone differs", () => {
    const instant = "2026-07-14T16:30:00Z";
    expect(calendarDateKey(instant, "Asia/Shanghai")).toBe("2026-07-15");
    expect(calendarDateKey(instant, "America/Los_Angeles")).toBe("2026-07-14");
  });
  test("only scheduled actions execute", () =>
    expect(
      executes(make("a", "2026-07-15T10:00:00Z", "scheduled_action")),
    ).toBe(true));
});
