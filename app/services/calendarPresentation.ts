import type { CalendarItem, CalendarKind } from "../store/calendar";
export const kindLabel: Record<CalendarKind, string> = {
  event: "Event",
  reminder: "Reminder",
  deadline: "Deadline",
  scheduled_action: "Zen action",
};
export function itemInstant(item: CalendarItem): string {
  return item.next_at ?? item.start_at ?? item.notify_at ?? item.due_at!;
}
export function viewerTimezone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
}
export function calendarDateKey(instant: string | Date, timeZone: string) {
  const parts = new Intl.DateTimeFormat("en-CA-u-ca-gregory-nu-latn", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(new Date(instant));
  const value = Object.fromEntries(
    parts
      .filter((part) => part.type !== "literal")
      .map((part) => [part.type, part.value]),
  );
  return `${value.year}-${value.month}-${value.day}`;
}
function dateOrdinal(key: string) {
  const [year, month, day] = key.split("-").map(Number);
  return Math.floor(Date.UTC(year, month - 1, day) / 86_400_000);
}
export function agendaSection(
  item: CalendarItem,
  now = new Date(),
  timeZone = viewerTimezone(),
): "Today" | "Tomorrow" | "Later This Week" | "Later" {
  const todayKey = calendarDateKey(now, timeZone);
  const days =
    dateOrdinal(calendarDateKey(itemInstant(item), timeZone)) -
    dateOrdinal(todayKey);
  if (days <= 0) return "Today";
  if (days === 1) return "Tomorrow";
  const [year, month, day] = todayKey.split("-").map(Number);
  const weekday = new Date(Date.UTC(year, month - 1, day)).getUTCDay();
  if (days <= 7 - weekday) return "Later This Week";
  return "Later";
}
export function groupAgenda(
  items: CalendarItem[],
  now = new Date(),
  timeZone = viewerTimezone(),
) {
  const labels = ["Today", "Tomorrow", "Later This Week", "Later"] as const;
  return labels
    .map((title) => ({
      title,
      data: items.filter(
        (item) => agendaSection(item, now, timeZone) === title,
      ),
    }))
    .filter((section) => section.data.length > 0);
}
export function formatCalendarTime(item: CalendarItem, locale?: string) {
  const value = new Date(itemInstant(item));
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    timeZone: item.timezone,
    timeZoneName: "short",
  }).format(value);
}
export function executes(item: CalendarItem) {
  return item.kind === "scheduled_action";
}
export function calendarReminderPlan(
  serverId: string,
  items: CalendarItem[],
  now = Date.now(),
) {
  return items
    .filter(
      (item) =>
        item.kind === "reminder" &&
        item.status === "scheduled" &&
        Boolean(item.next_at) &&
        Date.parse(item.next_at) > now,
    )
    .map((item) => ({
      key: `${serverId}:${item.id}`,
      item,
      trigger: item.next_at,
      fingerprint: JSON.stringify({
        trigger: item.next_at,
        title: item.title,
        body: item.notes?.trim() || `Reminder · ${item.timezone}`,
        timezone: item.timezone,
        revision: item.revision,
      }),
    }));
}
