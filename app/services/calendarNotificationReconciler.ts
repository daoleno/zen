import type { CalendarItem } from "../store/calendar";
import { calendarReminderPlan } from "./calendarPresentation";

export type CalendarNotificationRequest = {
  title: string;
  body: string;
  itemId: string;
  serverId: string;
  trigger: string;
};
export type CalendarNotificationDependencies = {
  getStored(): Promise<string | null>;
  setStored(value: string): Promise<void>;
  schedule(request: CalendarNotificationRequest): Promise<string>;
  cancel(id: string): Promise<void>;
  ensureChannel(): Promise<void>;
};
type StoredNotification = { id: string; fingerprint: string; trigger: string };
type NotificationMap = Record<string, StoredNotification>;

export async function reconcileCalendarNotifications(
  serverId: string,
  items: CalendarItem[],
  deps: CalendarNotificationDependencies,
  now = Date.now(),
) {
  await deps.ensureChannel();
  const current = await readMap(deps);
  const wanted = new Set<string>();
  for (const { key, item, trigger, fingerprint } of calendarReminderPlan(
    serverId,
    items,
    now,
  )) {
    wanted.add(key);
    const existing = current[key];
    if (existing?.fingerprint === fingerprint && existing.trigger === trigger) {
      continue;
    }
    if (existing?.id) {
      try {
        await deps.cancel(existing.id);
      } catch {}
    }
    const id = await deps.schedule({
      title: item.title,
      body: item.notes?.trim() || `Reminder · ${item.timezone}`,
      itemId: item.id,
      serverId,
      trigger,
    });
    current[key] = { id, fingerprint, trigger };
  }
  for (const [key, record] of Object.entries(current)) {
    if (!key.startsWith(`${serverId}:`) || wanted.has(key)) continue;
    try {
      await deps.cancel(record.id);
    } catch {}
    delete current[key];
  }
  await deps.setStored(JSON.stringify(current));
}

async function readMap(
  deps: Pick<CalendarNotificationDependencies, "getStored">,
): Promise<NotificationMap> {
  try {
    const parsed = JSON.parse((await deps.getStored()) || "{}") as Record<
      string,
      StoredNotification | string
    >;
    return Object.fromEntries(
      Object.entries(parsed).map(([key, value]) => [
        key,
        typeof value === "string"
          ? { id: value, fingerprint: "", trigger: "" }
          : value,
      ]),
    );
  } catch {
    return {};
  }
}
