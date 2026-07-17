import { describe, expect, test } from "bun:test";
import {
  foregroundNotificationPresentation,
  resolveNotificationDestination,
} from "./notificationRouting";

describe("remote notification behavior", () => {
  test("routes exact Terminal and Brain push identities", () => {
    expect(
      resolveNotificationDestination({
        screen: "terminal",
        agent_id: "agent-1",
        server_id: "server-1",
      }),
    ).toEqual({ kind: "terminal", agentId: "agent-1", serverId: "server-1" });

    expect(
      resolveNotificationDestination({
        screen: "brain",
        server_id: "server-1",
        brain_thread_id: "thread-frozen",
        brain_message_id: "calendar_result:item:run",
      }),
    ).toEqual({
      kind: "brain",
      serverId: "server-1",
      brainThreadId: "thread-frozen",
      brainMessageId: "calendar_result:item:run",
    });
  });

  test("retains Calendar and inbox deep-link behavior", () => {
    expect(
      resolveNotificationDestination({
        screen: "calendar",
        server_id: "server-1",
        calendar_id: "item-1",
      }),
    ).toEqual({
      kind: "calendar",
      serverId: "server-1",
      calendarId: "item-1",
    });
    expect(resolveNotificationDestination({ screen: "inbox" })).toEqual({
      kind: "inbox",
    });
    expect(resolveNotificationDestination({ screen: "unknown" })).toBeNull();
  });

  test("foreground remote notifications remain visible and audible", async () => {
    expect(await foregroundNotificationPresentation()).toEqual({
      shouldPlaySound: true,
      shouldSetBadge: false,
      shouldShowBanner: true,
      shouldShowList: true,
    });
  });
});
