export type NotificationDestination =
  | { kind: "terminal"; workerId: string; serverId: string }
  | { kind: "inbox" }
  | { kind: "calendar"; calendarId?: string; serverId?: string }
  | {
      kind: "brain";
      brainThreadId?: string;
      brainMessageId?: string;
      serverId?: string;
    };

export async function foregroundNotificationPresentation() {
  return {
    shouldPlaySound: true,
    shouldSetBadge: false,
    shouldShowBanner: true,
    shouldShowList: true,
  };
}

function optionalString(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}

export function resolveNotificationDestination(
  data: unknown,
): NotificationDestination | null {
  if (!data || typeof data !== "object") {
    return null;
  }
  const fields = data as Record<string, unknown>;
  const workerId = optionalString(fields.worker_id);
  const serverId = optionalString(fields.server_id);
  if (workerId && serverId) {
    return { kind: "terminal", workerId, serverId };
  }

  switch (fields.screen) {
    case "inbox":
      return { kind: "inbox" };
    case "calendar":
      return {
        kind: "calendar",
        calendarId: optionalString(fields.calendar_id),
        serverId,
      };
    case "brain":
      return {
        kind: "brain",
        brainThreadId: optionalString(fields.brain_thread_id),
        brainMessageId: optionalString(fields.brain_message_id),
        serverId,
      };
    default:
      return null;
  }
}
