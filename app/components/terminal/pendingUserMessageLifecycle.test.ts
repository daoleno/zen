// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  acknowledgePendingUserMessageWithStructuredTurns,
  canReconcilePendingAcknowledgementAgainstProjection,
  markPendingUserMessageDispatched,
  pendingUserMessageLifecycleLabel,
  PENDING_MESSAGE_RETRY_ACCESSIBILITY_LABEL,
  nextPendingUserMessagePruneAt,
  pendingUserMessageMaxAgeMs,
  PENDING_USER_MESSAGE_QUEUED_MAX_AGE_MS,
  PENDING_USER_MESSAGE_SENDING_MAX_AGE_MS,
  presentPendingUserMessageLifecycle,
  queuedOrdinalByPendingId,
  rejectPendingUserMessage,
  redispatchPendingUserMessageInSubmissionOrder,
  reconcilePendingUserMessagesAgainstEvents,
  reconcilePendingUserMessagesWithStructuredTurns,
  retainPendingUserMessages,
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
    turnId: overrides.turnId ?? `turn:${overrides.id}`,
    turnStartedAt:
      overrides.turnStartedAt ?? "2026-07-10T10:00:00.000Z",
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
    turnId: overrides.turnId ?? `turn:${overrides.id}`,
    turnStartedAt: overrides.turnStartedAt ?? overrides.createdAt,
    sentText: overrides.sentText ?? overrides.body,
    attachments: overrides.attachments ?? [],
    ...overrides,
  };
}

describe("pendingUserMessageLifecycleLabel", () => {
  test("failed Retry has a stable accessible action label", () => {
    expect(PENDING_MESSAGE_RETRY_ACCESSIBILITY_LABEL).toBe(
      "Retry sending message",
    );
  });
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
  test("assigns ordinals in submission order to every authoritative queued row", () => {
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
    expect(ordinals.get("d")).toBe(2);
    expect(presentPendingUserMessageLifecycle(messages[1]!, ordinals)).toMatchObject({
      label: "Queued 1/3",
      accessibilityLabel: "Queued, 1 of 3",
    });
    expect(presentPendingUserMessageLifecycle(messages[2]!, ordinals).label).toBe(
      "Queued 2/3",
    );
  });
});

describe("retention", () => {
  test("unconfirmed and failed rows never age out as unsent", () => {
    const createdAt = "2026-07-10T10:00:00.000Z";
    const muchLater = Date.parse(createdAt) + 24 * 60 * 60_000;
    expect(pendingUserMessageMaxAgeMs("unconfirmed")).toBe(Infinity);
    expect(pendingUserMessageMaxAgeMs("failed")).toBe(Infinity);
    expect(shouldPrunePendingUserMessageByLifecycle(
      { createdAt, lifecycle: "unconfirmed" },
      muchLater,
    )).toBe(false);
    expect(shouldPrunePendingUserMessageByLifecycle(
      { createdAt, lifecycle: "failed" },
      muchLater,
    )).toBe(false);
    expect(nextPendingUserMessagePruneAt([
      { createdAt, lifecycle: "unconfirmed" },
      { createdAt, lifecycle: "failed" },
    ], muchLater)).toBeUndefined();
    const many = Array.from({ length: 18 }, (_, index) => pending({
      id: `unknown-${index}`,
      body: `message ${index}`,
      lifecycle: index % 2 === 0 ? "unconfirmed" : "failed",
      createdAt,
    }));
    expect(retainPendingUserMessages(
      many,
      undefined,
      [],
      muchLater,
    )).toHaveLength(18);
  });

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

  test("settled-only rows do not schedule an infinite prune timer", () => {
    const createdAt = "2026-07-10T10:00:00.000Z";
    const createdMs = Date.parse(createdAt);
    expect(nextPendingUserMessagePruneAt([
      { createdAt, lifecycle: "settled" },
    ], createdMs)).toBeUndefined();
    expect(nextPendingUserMessagePruneAt([
      { createdAt, lifecycle: "settled" },
      { createdAt, lifecycle: "queued" },
    ], createdMs)).toBe(
      createdMs + PENDING_USER_MESSAGE_QUEUED_MAX_AGE_MS,
    );
  });

  test("finite prune deadlines remain deterministic for expired and invalid rows", () => {
    const now = Date.parse("2026-07-10T10:05:00.000Z");
    expect(nextPendingUserMessagePruneAt([], now)).toBeUndefined();
    expect(nextPendingUserMessagePruneAt([
      { createdAt: "2026-07-10T10:00:00.000Z", lifecycle: "sending" },
    ], now)).toBe(
      Date.parse("2026-07-10T10:00:00.000Z") +
        PENDING_USER_MESSAGE_SENDING_MAX_AGE_MS,
    );
    expect(nextPendingUserMessagePruneAt([
      { createdAt: "invalid", lifecycle: "sending" },
    ], now)).toBe(now + PENDING_USER_MESSAGE_SENDING_MAX_AGE_MS);
  });

  test("authoritative current and queue entries survive age and the orphan cap", () => {
    const createdAt = "2026-07-10T10:00:00.000Z";
    const messages = Array.from({ length: 18 }, (_, index) =>
      pending({
        id: `p${index}`,
        turnId: `turn-${index}`,
        body: `message ${index}`,
        lifecycle: index === 0 ? "sending" : "queued",
        createdAt,
        acceptedAt: createdAt,
      })
    );
    const retained = retainPendingUserMessages(
      messages,
      {
        id: "turn-0",
        status: "running",
        started_at: createdAt,
      },
      messages.slice(1).map((message) => ({
        id: message.turnId,
        status: "queued" as const,
        started_at: message.createdAt,
      })),
      Date.parse(createdAt) + PENDING_USER_MESSAGE_QUEUED_MAX_AGE_MS + 1,
    );
    expect(retained.map((message) => message.id)).toEqual(
      messages.map((message) => message.id),
    );
  });
});

describe("dispatch acknowledgement and retry precedence", () => {
  test("correlated rejection is inline failed, but stale rejection cannot regress retry", () => {
    const initial = pending({
      id: "p1",
      body: "run tests",
      lifecycle: "unconfirmed",
      dispatchRequestId: "request-1",
      lastAttemptAt: "2026-07-10T10:00:00.000Z",
    });
    const failed = rejectPendingUserMessage(initial, {
      requestId: "request-1",
      code: "structured_lifecycle_syncing",
      message: "Refresh and retry.",
      failedAt: "2026-07-10T10:00:01.000Z",
    });
    expect(failed).toMatchObject({
      id: "p1",
      turnId: initial.turnId,
      turnStartedAt: initial.turnStartedAt,
      lifecycle: "failed",
      failureCode: "structured_lifecycle_syncing",
      failureMessage: "Refresh and retry.",
    });

    const retried = markPendingUserMessageDispatched(failed, {
      requestId: "request-2",
      attemptedAt: "2026-07-10T10:00:03.000Z",
      queuedHint: true,
      createdAfterMaxSeq: 12,
      createdAfterEventIds: ["event-12"],
    });
    expect(retried).toMatchObject({
      id: "p1",
      turnId: initial.turnId,
      turnStartedAt: initial.turnStartedAt,
      sentText: initial.sentText,
      lifecycle: "unconfirmed",
      dispatchRequestId: "request-2",
      queuedHint: true,
      createdAt: "2026-07-10T10:00:03.000Z",
      createdAfterMaxSeq: 12,
      createdAfterEventIds: ["event-12"],
    });
    expect(retried.failureMessage).toBeUndefined();
    expect(rejectPendingUserMessage(retried, {
      requestId: "request-1",
      code: "stale",
      message: "late rejection",
      failedAt: "2026-07-10T10:00:04.000Z",
    })).toBe(retried);
  });

  test("retry keeps one row and durable turn identity but moves to real submission order", () => {
    const first = pending({
      id: "rejected-first",
      turnId: "turn-first",
      turnStartedAt: "2026-07-10T10:00:00.000Z",
      body: "first",
      lifecycle: "failed",
      dispatchRequestId: "request-1",
    });
    const later = pending({
      id: "accepted-later",
      turnId: "turn-later",
      body: "later",
      lifecycle: "queued",
    });
    const retried = redispatchPendingUserMessageInSubmissionOrder(
      [first, later],
      first.id,
      {
        requestId: "request-2",
        attemptedAt: "2026-07-10T10:00:05.000Z",
        queuedHint: true,
      },
    );
    expect(retried.map((message) => message.id)).toEqual([
      "accepted-later",
      "rejected-first",
    ]);
    expect(retried[1]).toMatchObject({
      turnId: "turn-first",
      turnStartedAt: "2026-07-10T10:00:00.000Z",
      lifecycle: "unconfirmed",
      dispatchRequestId: "request-2",
    });
  });

  test("stale ACK is ignored and authoritative active projection overrides failure", () => {
    const retried = pending({
      id: "p1",
      turnId: "turn-1",
      body: "work",
      lifecycle: "unconfirmed",
      dispatchRequestId: "request-2",
    });
    expect(acknowledgePendingUserMessageWithStructuredTurns(retried, {
      requestId: "request-1",
      turnId: "turn-1",
      lifecycle: "sending",
      acceptedAt: "2026-07-10T10:00:01.000Z",
    })).toBe(retried);

    const failed = { ...retried, lifecycle: "failed" as const,
      failureMessage: "not accepted", failureCode: "sync" };
    expect(reconcilePendingUserMessagesWithStructuredTurns(
      [failed],
      {
        id: "turn-1",
        status: "running",
        started_at: failed.turnStartedAt,
      },
      [],
      "daemon-a",
      5,
    )).toMatchObject([{
      lifecycle: "sending",
      authoritativeActiveObserved: true,
      failureMessage: undefined,
      failureCode: undefined,
    }]);
  });

  test("failed row renders one accessible inline Retry and echo dedupes it", () => {
    let retriedId: string | undefined;
    const message = pendingMessage({
      id: "pending-failed",
      turnId: "turn-failed",
      body: "same",
      sentText: "same",
      lifecycle: "failed",
      createdAt: "2026-07-10T10:00:01.000Z",
      dispatchRequestId: "request-failed",
      failureMessage: "Lifecycle is refreshing.",
      queuedHint: true,
    });
    const merged = mergePendingUserMessagesIntoTimeline(
      [{
        type: "message",
        id: "echo-failed",
        role: "user",
        body: "same",
        timestamp: "2026-07-10T10:00:02.000Z",
        attachments: [],
      }],
      [{ ...message, confirmedEventId: "echo-failed" }],
      (id) => {
        retriedId = id;
      },
    );
    expect(merged).toHaveLength(1);
    expect(merged[0]).toMatchObject({
      id: "pending-failed",
      pending: true,
      pendingLifecycle: "failed",
      pendingLifecycleLabel: "Not accepted",
      pendingLifecycleAccessibilityLabel: "Message not accepted",
      pendingFailureMessage: "Lifecycle is refreshing.",
    });
    expect(typeof merged[0]?.onRetryPending).toBe("function");
    merged[0]?.onRetryPending?.();
    expect(retriedId).toBe("pending-failed");
  });
});

describe("reconcilePendingUserMessagesAgainstEvents", () => {
  test("records echo identities without letting transcript metadata retire queue state", () => {
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
    expect(
      reconciled.map(({ id, confirmedEventId }) => ({ id, confirmedEventId })),
    ).toEqual([
      { id: "p1", confirmedEventId: "echo-1" },
      { id: "p2", confirmedEventId: "echo-2" },
    ]);
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
      reconcilePendingUserMessagesAgainstEvents(pendingMessages, events)[0],
    ).toMatchObject({ id: "p1", confirmedEventId: "echo" });
  });

  test("reserves successive identical echoes in queue order without duplicate claims", () => {
    const pendingMessages = [
      pending({
        id: "p1",
        turnId: "turn-1",
        body: "same follow-up",
        lifecycle: "queued",
        createdAfterMaxSeq: 10,
      }),
      pending({
        id: "p2",
        turnId: "turn-2",
        body: "same follow-up",
        lifecycle: "queued",
        createdAfterMaxSeq: 10,
      }),
    ];
    const afterFirstEcho = reconcilePendingUserMessagesAgainstEvents(
      pendingMessages,
      [
        {
          id: "echo-1",
          seq: 11,
          kind: "user_message",
          body: "same follow-up",
        },
      ],
    );
    expect(
      afterFirstEcho.map(({ id, confirmedEventId }) => ({
        id,
        confirmedEventId,
      })),
    ).toEqual([
      { id: "p1", confirmedEventId: "echo-1" },
      { id: "p2", confirmedEventId: undefined },
    ]);

    expect(
      reconcilePendingUserMessagesAgainstEvents(afterFirstEcho, [
        {
          id: "echo-1",
          seq: 11,
          kind: "user_message",
          body: "same follow-up",
        },
      ]).map(({ id, confirmedEventId }) => ({ id, confirmedEventId })),
    ).toEqual([
      { id: "p1", confirmedEventId: "echo-1" },
      { id: "p2", confirmedEventId: undefined },
    ]);

    expect(
      reconcilePendingUserMessagesAgainstEvents(afterFirstEcho, [
        {
          id: "echo-1",
          seq: 11,
          kind: "user_message",
          body: "same follow-up",
        },
        {
          id: "echo-2",
          seq: 12,
          kind: "user_message",
          body: "same follow-up",
        },
      ]).map(({ id, confirmedEventId }) => ({ id, confirmedEventId })),
    ).toEqual([
      { id: "p1", confirmedEventId: "echo-1" },
      { id: "p2", confirmedEventId: "echo-2" },
    ]);
  });

  test("dedupes attachment-only echoes in submission order", () => {
    const attachmentText =
      '<zen_attachments>{"files":[{"name":"a.png","path":"/tmp/a.png"}]}</zen_attachments>';
    const pendingMessages = [
      pending({
        id: "p1",
        body: "",
        sentText: attachmentText,
        lifecycle: "queued",
        createdAfterMaxSeq: 10,
        attachments: [{ path: "/tmp/a.png" }],
      }),
      pending({
        id: "p2",
        body: "",
        sentText: attachmentText,
        lifecycle: "queued",
        createdAfterMaxSeq: 10,
        attachments: [{ path: "/tmp/a.png" }],
      }),
    ];
    const first = reconcilePendingUserMessagesAgainstEvents(
      pendingMessages,
      [{ id: "echo-1", seq: 11, kind: "user_message", body: attachmentText }],
    );
    expect(first.map(({ id, confirmedEventId }) => ({ id, confirmedEventId }))).toEqual([
      { id: "p1", confirmedEventId: "echo-1" },
      { id: "p2", confirmedEventId: undefined },
    ]);
    expect(
      reconcilePendingUserMessagesAgainstEvents(first, [
        { id: "echo-1", seq: 11, kind: "user_message", body: attachmentText },
        { id: "echo-2", seq: 12, kind: "user_message", body: attachmentText },
      ]).map(({ id, confirmedEventId }) => ({ id, confirmedEventId })),
    ).toEqual([
      { id: "p1", confirmedEventId: "echo-1" },
      { id: "p2", confirmedEventId: "echo-2" },
    ]);
  });
});

describe("mergePendingUserMessagesIntoTimeline", () => {
  test("busy queue shows ordered queue positions in submission order", () => {
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
      pendingLifecycleLabel: "Queued 1/2",
    });
    expect(pendingItems[1]).toMatchObject({
      id: "pending-b",
      body: "follow up two",
      pendingLifecycle: "queued",
      pendingLifecycleLabel: "Queued 2/2",
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

  test("attachment-only echo claims the optimistic item without duplication", () => {
    const attachmentText =
      '<zen_attachments>{"files":[{"name":"a.png","path":"/tmp/a.png"}]}</zen_attachments>';
    const merged = mergePendingUserMessagesIntoTimeline(
      [
        {
          type: "message",
          id: "echo-attachment",
          role: "user",
          timestamp: "2026-07-10T10:00:01.000Z",
          body: "",
          attachments: [{ name: "a.png", path: "/tmp/a.png" }],
        },
      ],
      [
        pendingMessage({
          id: "pending-attachment",
          body: "",
          sentText: attachmentText,
          attachments: [{ name: "a.png", path: "/tmp/a.png" }],
          lifecycle: "queued",
          createdAt: "2026-07-10T10:00:00.000Z",
        }),
      ],
    );
    const userMessages = merged.filter(
      (item) => item.type === "message" && item.role === "user",
    );
    expect(userMessages).toHaveLength(1);
    expect(userMessages[0]).toMatchObject({
      id: "pending-attachment",
      attachments: [{ path: "/tmp/a.png" }],
    });
    expect(userMessages[0]).toMatchObject({
      pending: true,
      pendingLifecycle: "queued",
      pendingLifecycleLabel: "Queued next",
    });
  });

  test("pending rendering never synthesizes a Working placeholder", () => {
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
    expect(merged.some((item) => item.type === "activity" && item.title === "Working")).toBe(false);
  });
});

describe("server queue reconciliation", () => {
  test("ack reconciliation waits for the same daemon epoch at or after its revision", () => {
    const acknowledgement = {
      turnEpoch: "daemon-b",
      turnRevision: 7,
    };
    expect(canReconcilePendingAcknowledgementAgainstProjection(
      acknowledgement,
      { turn_epoch: "daemon-a", turn_revision: 99 },
    )).toBe(false);
    expect(canReconcilePendingAcknowledgementAgainstProjection(
      acknowledgement,
      { turn_epoch: "daemon-b", turn_revision: 6 },
    )).toBe(false);
    expect(canReconcilePendingAcknowledgementAgainstProjection(
      acknowledgement,
      { turn_epoch: "daemon-b", turn_revision: 7 },
    )).toBe(true);
  });

  test("uses oldest-first server order and promotes the active item to sending", () => {
    const messages = [
      pending({ id: "b", turnId: "turn-b", body: "second", lifecycle: "queued" }),
      pending({ id: "a", turnId: "turn-a", body: "first", lifecycle: "sending" }),
      pending({ id: "c", turnId: "turn-c", body: "third", lifecycle: "sending" }),
    ];
    const reconciled = reconcilePendingUserMessagesWithStructuredTurns(
      messages,
      {
        id: "turn-a",
        status: "running",
        started_at: "2026-07-10T10:00:00.000Z",
      },
      [
        {
          id: "turn-b",
          status: "queued",
          started_at: "2026-07-10T10:00:01.000Z",
        },
        {
          id: "turn-c",
          status: "queued",
          started_at: "2026-07-10T10:00:02.000Z",
        },
      ],
    );
    expect(reconciled.map(({ id, lifecycle }) => ({ id, lifecycle }))).toEqual([
      { id: "a", lifecycle: "sending" },
      { id: "b", lifecycle: "queued" },
      { id: "c", lifecycle: "queued" },
    ]);
  });

  test("late queued acknowledgement cannot regress an already promoted turn", () => {
    const message = pending({
      id: "b",
      turnId: "turn-b",
      body: "next",
      lifecycle: "sending",
      authoritativeActiveObserved: true,
    });
    const acknowledged = acknowledgePendingUserMessageWithStructuredTurns(
      message,
      {
        turnId: "turn-b",
        lifecycle: "queued",
        acceptedAt: "2026-07-10T10:00:02.000Z",
      },
      {
        id: "turn-b",
        status: "running",
        started_at: "2026-07-10T10:00:01.000Z",
      },
      [],
    );
    expect(acknowledged).toMatchObject({
      lifecycle: "sending",
      authoritativeActiveObserved: true,
    });
  });

  test("queued acknowledgement survives an older snapshot and settles after a fast queue drain", () => {
    const startedAt = "2026-07-10T10:00:02.000Z";
    const acknowledged = acknowledgePendingUserMessageWithStructuredTurns(
      pending({
        id: "b",
        turnId: "turn-b",
        turnStartedAt: startedAt,
        body: "middle",
        lifecycle: "sending",
      }),
      {
        turnId: "turn-b",
        lifecycle: "queued",
        acceptedAt: startedAt,
        turnEpoch: "daemon-a",
        turnRevision: 5,
      },
      {
        id: "turn-a",
        status: "running",
        // A future-skewed public clock cannot make this old projection causal.
        started_at: "2026-07-10T11:00:01.000Z",
      },
      [],
    );
    expect(acknowledged).toMatchObject({
      lifecycle: "queued",
      authoritativeQueueObserved: true,
      authoritativeLifecycleEpoch: "daemon-a",
      authoritativeLifecycleRevision: 5,
    });
    expect(reconcilePendingUserMessagesWithStructuredTurns(
      [acknowledged],
      {
        id: "turn-a",
        status: "running",
        started_at: "2026-07-10T11:00:01.000Z",
      },
      [],
      "daemon-a",
      5,
    )).toHaveLength(1);
    expect(reconcilePendingUserMessagesWithStructuredTurns(
      [acknowledged],
      {
        id: "turn-c",
        status: "completed",
        started_at: "2026-07-10T10:00:03.000Z",
        settled_at: "2026-07-10T10:00:04.000Z",
      },
      [],
      "daemon-a",
      8,
    )).toMatchObject([{ id: "b", lifecycle: "settled" }]);
  });

  test("a new daemon lifecycle epoch settles unconfirmed accepted state without losing its bubble", () => {
    const acknowledged = acknowledgePendingUserMessageWithStructuredTurns(
      pending({
        id: "a",
        turnId: "turn-a",
        body: "work",
        lifecycle: "sending",
      }),
      {
        turnId: "turn-a",
        lifecycle: "sending",
        acceptedAt: "2026-07-10T10:00:00.000Z",
        turnEpoch: "daemon-a",
        turnRevision: 4,
      },
    );
    expect(reconcilePendingUserMessagesWithStructuredTurns(
      [acknowledged],
      undefined,
      [],
      "daemon-b",
      0,
    )).toMatchObject([{ id: "a", lifecycle: "settled" }]);
  });

  test("terminal-before-echo preserves identical order and prevents successor echo theft", () => {
    const first = pending({
      id: "p1",
      turnId: "turn-1",
      body: "same",
      lifecycle: "sending",
      acceptedAt: "2026-07-10T10:00:00.000Z",
      authoritativeActiveObserved: true,
      authoritativeLifecycleEpoch: "daemon-a",
      authoritativeLifecycleRevision: 4,
    });
    const second = pending({
      id: "p2",
      turnId: "turn-2",
      body: "same",
      lifecycle: "queued",
      acceptedAt: "2026-07-10T10:00:01.000Z",
      authoritativeQueueObserved: true,
      authoritativeLifecycleEpoch: "daemon-a",
      authoritativeLifecycleRevision: 5,
    });
    const afterTerminal = reconcilePendingUserMessagesWithStructuredTurns(
      [first, second],
      {
        id: "turn-1",
        status: "completed",
        started_at: first.turnStartedAt,
        settled_at: "2026-07-10T10:00:02.000Z",
      },
      [{
        id: "turn-2",
        status: "queued",
        started_at: second.turnStartedAt,
      }],
      "daemon-a",
      6,
    );
    expect(afterTerminal.map(({ id, lifecycle }) => ({ id, lifecycle }))).toEqual([
      { id: "p1", lifecycle: "settled" },
      { id: "p2", lifecycle: "queued" },
    ]);

    const withDelayedFirstEcho = reconcilePendingUserMessagesAgainstEvents(
      afterTerminal,
      [{ id: "echo-1", seq: 11, kind: "user_message", body: "same" }],
    );
    expect(withDelayedFirstEcho.map(({ id, confirmedEventId }) => ({
      id,
      confirmedEventId,
    }))).toEqual([
      { id: "p1", confirmedEventId: "echo-1" },
      { id: "p2", confirmedEventId: undefined },
    ]);

    const afterPromotion = reconcilePendingUserMessagesWithStructuredTurns(
      withDelayedFirstEcho,
      {
        id: "turn-2",
        status: "running",
        started_at: second.turnStartedAt,
      },
      [],
      "daemon-a",
      7,
    );
    expect(afterPromotion).toHaveLength(1);
    expect(afterPromotion[0]).toMatchObject({
      id: "p2",
      createdAfterEventIds: ["echo-1"],
    });
  });

  test("promotion retires a confirmed row and reserves its echo for identical successors", () => {
    const messages = [
      pending({
        id: "p1",
        turnId: "turn-1",
        body: "same",
        lifecycle: "queued",
        confirmedEventId: "echo-1",
        authoritativeQueueObserved: true,
      }),
      pending({
        id: "p2",
        turnId: "turn-2",
        body: "same",
        lifecycle: "queued",
        authoritativeQueueObserved: true,
      }),
    ];
    const promoted = reconcilePendingUserMessagesWithStructuredTurns(
      messages,
      {
        id: "turn-1",
        status: "running",
        started_at: "2026-07-10T10:00:00.000Z",
      },
      [{
        id: "turn-2",
        status: "queued",
        started_at: "2026-07-10T10:00:01.000Z",
      }],
    );
    expect(promoted).toHaveLength(1);
    expect(promoted[0]).toMatchObject({
      id: "p2",
      createdAfterEventIds: ["echo-1"],
    });
    expect(reconcilePendingUserMessagesAgainstEvents(promoted, [{
      id: "echo-1",
      seq: 11,
      kind: "user_message",
      body: "same",
    }])[0]?.confirmedEventId).toBeUndefined();
    expect(reconcilePendingUserMessagesAgainstEvents(promoted, [
      { id: "echo-1", seq: 11, kind: "user_message", body: "same" },
      { id: "echo-2", seq: 12, kind: "user_message", body: "same" },
    ])[0]?.confirmedEventId).toBe("echo-2");
  });
});
