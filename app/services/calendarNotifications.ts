import AsyncStorage from "@react-native-async-storage/async-storage";
import * as Notifications from "expo-notifications";
import { Platform } from "react-native";
import type { CalendarItem } from "../store/calendar";
import {
  reconcileCalendarNotifications,
  type CalendarNotificationDependencies,
} from "./calendarNotificationReconciler";

const storageKey = "zen.calendar.notification_ids.v1";
export type CalendarNotificationState =
  "granted" | "denied" | "undetermined" | "unavailable";

export async function calendarNotificationState(): Promise<CalendarNotificationState> {
  try {
    const permission = await Notifications.getPermissionsAsync();
    if (permission.status === "granted") return "granted";
    if (permission.status === "denied") return "denied";
    return "undetermined";
  } catch {
    return "unavailable";
  }
}

export async function requestCalendarNotifications(): Promise<CalendarNotificationState> {
  try {
    const permission = await Notifications.requestPermissionsAsync();
    if (permission.status === "granted") return "granted";
    if (permission.status === "denied") return "denied";
    return "undetermined";
  } catch {
    return "unavailable";
  }
}

export async function syncCalendarNotifications(
  serverId: string,
  items: CalendarItem[],
): Promise<CalendarNotificationState> {
  const state = await calendarNotificationState();
  if (state !== "granted") return state;
  await reconcileCalendarNotifications(serverId, items, notificationDeps());
  return state;
}

export async function cancelCalendarNotifications(serverId: string) {
  await reconcileCalendarNotifications(serverId, [], notificationDeps());
}

function notificationDeps(): CalendarNotificationDependencies {
  return {
    getStored: () => AsyncStorage.getItem(storageKey),
    setStored: (value) => AsyncStorage.setItem(storageKey, value),
    schedule: (request) =>
      Notifications.scheduleNotificationAsync({
        content: {
          title: request.title,
          body: request.body,
          data: {
            screen: "calendar",
            calendar_id: request.itemId,
            server_id: request.serverId,
          },
        },
        trigger: {
          type: Notifications.SchedulableTriggerInputTypes.DATE,
          date: new Date(request.trigger),
          channelId: Platform.OS === "android" ? "zen-calendar" : undefined,
        },
      }),
    cancel: (id) => Notifications.cancelScheduledNotificationAsync(id),
    ensureChannel: async () => {
      if (Platform.OS === "android") {
        await Notifications.setNotificationChannelAsync("zen-calendar", {
          name: "Calendar reminders",
          importance: Notifications.AndroidImportance.HIGH,
        });
      }
    },
  };
}
