// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  classifyPendingUserMessageLifecycle,
  pendingUserMessageLifecycleLabel,
  pendingUserMessageMaxAgeMs,
  PENDING_USER_MESSAGE_QUEUED_MAX_AGE_MS,
  PENDING_USER_MESSAGE_SENDING_MAX_AGE_MS,
  presentPendingUserMessageLifecycle,
  queuedOrdinalByPendingId,
  reconcilePendingUserMessagesAgainstEvents,
  shouldPrunePendingUserMessageByLifecycle,
  type PendingUserMessageLifecycleFields,
} from "./pendingUserMessageLifecycle";
import { mergePendingUserMessagesIntoTimeline } from "./CodexTimelineModel";
import type { PendingUserMessage } from "./CodexChatSession";
import type { ZenTimelineItem } from "./CodexTimelineItemView";

function pending(
  overrides: Partial<PendingUserMessageLifecycleFields> &
    Pick<PendingUserMessageLifecycleFields, "id" | "body" | "lifecycle">,
): PendingUserMessageLifecycleFields {
  return {
    sentText: overrides.sentText ?? overrides.body,
    createdAt: overrides.createdAt ?? "2026-07-10T10:00:00.000Z",
    ...overrides,
  };
}

function pendingMessage(
  overrides: Partial<PendingUserMessage> &
    Pick<PendingUserMessage, "id" | "body" | "lifecycle" | "createdAt">,
): PendingUserMessage {
  return {
    sentText: overrides.sentText ?? overrides.body,
    attachments: overrides.attachments ?? [],
    ...overrides,
  };
}

describe("classifyPendingUserMessageLifecycle", () => {
  test("idle turn is sending", () => {
    expect(classifyPendingUserMessageLifecycle(false)).toBe("sending");
  });

  test("busy turn is queued", () => {
    expect(classifyPendingUserMessageLifecycle(true)).toBe("queued");
  });
});

describe("pendingUserMessageLifecycleLabel", () => {
  test("sending label", () => {
    expect(pendingUserMessageLifecycleLabel("sending")).toBe("Sending");
  });

  test("first queued is Queued next", () => {
    expect(pendingUserMessageLifecycleLabel("queued", 0)).toBe("Queued next");
  });

  test("later queued stays Queued without numbered badges", () => {
    expect(pendingUserMessageLifecycleLabel("queued", 1)).toBe("Queued");
    expect(pendingUserMessageLifecycleLabel("queued", 2)).toBe("Queued");
  });
});

describe("queued ordinals and presentation", () => {
  test("assigns ordinals in submission order among queued only", () => {
    const messages = [
      pending({ id: "a", body: "one", lifecycle: "sending" }),
      pending({ id: "b", body: "two", lifecycle: "queued" }),
      pending({ id: "c", body: "three", lifecycle: "queued" }),
      pending({
        id: "d",
        body: "four",
        lifecycle: "queued",
        confirmedEventId: "evt-1",
      }),
    ];
    const ordinals = queuedOrdinalByPendingId(messages);
    expect(ordinals.get("a")).toBeUndefined();
    expect(ordinals.get("b")).toBe(0);
    expect(ordinals.get("c")).toBe(1);
    expect(ordinals.get("d")).toBeUndefined();
    expect(presentPendingUserMessageLifecycle(messages[1]!, ordinals).label).toBe(
      "Queued next",
    );
    expect(presentPendingUserMessageLifecycle(messages[2]!, ordinals).label).toBe(
      "Queued",
    );
  });
});

describe("retention", () => {
  test("sending prunes after short max age", () => {
    const createdAt = "2026-07-10T10:00:00.000Z";
    const createdMs = Date.parse(createdAt);
    expect(
      shouldPrunePendingUserMessageByLifecycle(
        { createdAt, lifecycle: "sending" },
        createdMs + PENDING_USER_MESSAGE_SENDING_MAX_AGE_MS,
      ),
    ).toBe(false);
    expect(
      shouldPrunePendingUserMessageByLifecycle(
        { createdAt, lifecycle: "sending" },
        createdMs + PENDING_USER_MESSAGE_SENDING_MAX_AGE_MS + 1,
      ),
    ).toBe(true);
  });

  test("queued survives past sending max age but not past bounded max", () => {
    const createdAt = "2026-07-10T10:00:00.000Z";
    const createdMs = Date.parse(createdAt);
    expect(pendingUserMessageMaxAgeMs("queued")).toBe(
      PENDING_USER_MESSAGE_QUEUED_MAX_AGE_MS,
    );
    expect(
      shouldPrunePendingUserMessageByLifecycle(
        { createdAt, lifecycle: "queued" },
        createdMs + PENDING_USER_MESSAGE_SENDING_MAX_AGE_MS + 1,
      ),
    ).toBe(false);
    expect(
      shouldPrunePendingUserMessageByLifecycle(
        { createdAt, lifecycle: "queued" },
        createdMs + PENDING_USER_MESSAGE_QUEUED_MAX_AGE_MS + 1,
      ),
    ).toBe(true);
  });
});

describe("reconcilePendingUserMessagesAgainstEvents", () => {
  test("removes pending when echo matches without duplicates", () => {
    const pendingMessages = [
      pending({
        id: "p1",
        body: "hello",
        lifecycle: "queued",
        createdAfterMaxSeq: 10,
        createdAfterEventIds: ["old"],
      }),
      pending({
        id: "p2",
        body: "hello",
        lifecycle: "queued",
        createdAfterMaxSeq: 10,
        createdAfterEventIds: ["old"],
      }),
    ];
    const events = [
      { id: "old", seq: 5, kind: "user_message", body: "hello" },
      { id: "echo-1", seq: 11, kind: "user_message", body: "hello" },
      { id: "echo-2", seq: 12, kind: "user_message", body: "hello" },
    ];
    const reconciled = reconcilePendingUserMessagesAgainstEvents(
      pendingMessages,
      events,
    );
    expect(reconciled.map((message) => message.id)).toEqual([]);
  });

  test("keeps pending when only older matching events exist", () => {
    const pendingMessages = [
      pending({
        id: "p1",
        body: "next turn",
        lifecycle: "queued",
        createdAfterMaxSeq: 20,
        createdAfterEventIds: ["prior"],
      }),
    ];
    const events = [
      { id: "prior", seq: 20, kind: "user_message", body: "next turn" },
    ];
    const reconciled = reconcilePendingUserMessagesAgainstEvents(
      pendingMessages,
      events,
    );
    expect(reconciled.map((message) => message.id)).toEqual(["p1"]);
  });

  test("matches sentText including attachment payload stripping", () => {
    const pendingMessages = [
      pending({
        id: "p1",
        body: "with file",
        sentText:
          'with file\n\n<zen_attachments>{"files":[{"name":"a.png","path":"/tmp/a.png"}]}</zen_attachments>',
        lifecycle: "sending",
        createdAfterMaxSeq: 0,
      }),
    ];
    const events = [
      {
        id: "echo",
        seq: 1,
        kind: "user_message",
        body: "with file",
      },
    ];
    expect(
      reconcilePendingUserMessagesAgainstEvents(pendingMessages, events),
    ).toEqual([]);
  });
});

describe("mergePendingUserMessagesIntoTimeline", () => {
  test("busy queue shows Queued next then Queued in submission order", () => {
    const timeline: ZenTimelineItem[] = [
      {
        type: "message",
        id: "user-1",
        role: "user",
        timestamp: "2026-07-10T09:59:00.000Z",
        body: "start the long task",
        attachments: [],
      },
      {
        type: "activity",
        id: "act-1",
        timestamp: "2026-07-10T09:59:01.000Z",
        statusKey: "running",
        title: "Working",
        tone: "running",
        icon: "time-outline",
        defaultExpanded: false,
      },
    ];
    const pendingUserMessages = [
      pendingMessage({
        id: "pending-a",
        body: "follow up one",
        lifecycle: "queued",
        createdAt: "2026-07-10T10:00:01.000Z",
      }),
      pendingMessage({
        id: "pending-b",
        body: "follow up two",
        lifecycle: "queued",
        createdAt: "2026-07-10T10:00:02.000Z",
      }),
    ];
    const merged = mergePendingUserMessagesIntoTimeline(
      timeline,
      pendingUserMessages,
    );
    const pendingItems = merged.filter(
      (item) => item.type === "message" && item.pending,
    );
    expect(pendingItems).toHaveLength(2);
    expect(pendingItems[0]).toMatchObject({
      id: "pending-a",
      body: "follow up one",
      pendingLifecycle: "queued",
      pendingLifecycleLabel: "Queued next",
    });
    expect(pendingItems[1]).toMatchObject({
      id: "pending-b",
      body: "follow up two",
      pendingLifecycle: "queued",
      pendingLifecycleLabel: "Queued",
    });
  });

  test("idle submit shows Sending not Queued", () => {
    const merged = mergePendingUserMessagesIntoTimeline(
      [],
      [
        pendingMessage({
          id: "pending-send",
          body: "hello",
          lifecycle: "sending",
          createdAt: "2026-07-10T10:00:00.000Z",
        }),
      ],
    );
    expect(merged[0]).toMatchObject({
      pending: true,
      pendingLifecycle: "sending",
      pendingLifecycleLabel: "Sending",
    });
  });

  test("echo match claims identity without pending label", () => {
    const merged = mergePendingUserMessagesIntoTimeline(
      [
        {
          type: "message",
          id: "echo-1",
          role: "user",
          timestamp: "2026-07-10T10:00:01.000Z",
          body: "hello",
          attachments: [],
        },
      ],
      [
        pendingMessage({
          id: "pending-send",
          body: "hello",
          lifecycle: "sending",
          createdAt: "2026-07-10T10:00:00.000Z",
        }),
      ],
    );
    const userMessages = merged.filter(
      (item) => item.type === "message" && item.role === "user",
    );
    expect(userMessages).toHaveLength(1);
    expect(userMessages[0]).toMatchObject({
      id: "pending-send",
      body: "hello",
    });
    expect((userMessages[0] as { pending?: boolean }).pending).toBeUndefined();
  });
});
