import { describe, expect, test } from "bun:test";
import {
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
    Pick<PendingUserMessage, "id" | "body" | "lifecycle">,
): PendingUserMessage {
  const createdAt = overrides.createdAt ?? "2026-07-10T10:00:00.000Z";
  return {
    turnId: overrides.turnId ?? `turn:${overrides.id}`,
    turnStartedAt: overrides.turnStartedAt ?? createdAt,
    sentText: overrides.sentText ?? overrides.body,
    attachments: overrides.attachments ?? [],
    createdAt,
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
  test("assigns ordinals in submission order to every canonical queued row", () => {
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

  test("ACK-decorated rows survive age and the orphan cap until canonical upsert", () => {
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

  test("retry keeps one row durable identity and original canonical position", () => {
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
      "rejected-first",
      "accepted-later",
    ]);
    expect(retried[0]).toMatchObject({
      turnId: "turn-first",
      turnStartedAt: "2026-07-10T10:00:00.000Z",
      lifecycle: "unconfirmed",
      dispatchRequestId: "request-2",
    });
  });

  test("failed row overlays only its explicitly confirmed canonical event", () => {
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
      id: "echo-failed",
      pending: true,
      pendingLifecycle: "failed",
      pendingLifecycleLabel: "Not accepted",
      pendingLifecycleAccessibilityLabel: "Message not accepted",
      pendingFailureMessage: "Lifecycle is refreshing.",
    });
    const retryItem = merged[0];
    expect(retryItem?.type).toBe("message");
    if (retryItem?.type !== "message") {
      throw new Error("expected failed Submission to remain a message row");
    }
    expect(typeof retryItem.onRetryPending).toBe("function");
    retryItem.onRetryPending?.();
    expect(retriedId).toBe("pending-failed");
  });

  test("canonical rejection decorates the same optimistic ID when its rejection ACK is lost", () => {
    let retriedId: string | undefined;
    const merged = mergePendingUserMessagesIntoTimeline(
      [{
        type: "message",
        id: "submission-rejected",
        role: "user",
        body: "same immutable input",
        attachments: [],
        pending: true,
        pendingLifecycle: "failed",
        pendingLifecycleLabel: "Not accepted",
        pendingLifecycleAccessibilityLabel: "Message not accepted",
      }],
      [pendingMessage({
        id: "submission-rejected",
        turnId: "submission-rejected",
        body: "same immutable input",
        lifecycle: "unconfirmed",
      })],
      (id) => {
        retriedId = id;
      },
    );

    expect(merged).toHaveLength(1);
    expect(merged[0]).toMatchObject({
      id: "submission-rejected",
      pending: true,
      pendingLifecycle: "failed",
      pendingLifecycleLabel: "Not accepted",
      pendingLifecycleAccessibilityLabel: "Message not accepted",
    });
    const retryItem = merged[0];
    expect(retryItem?.type).toBe("message");
    if (retryItem?.type !== "message") {
      throw new Error("expected rejected Submission to remain a message row");
    }
    expect(typeof retryItem.onRetryPending).toBe("function");
    retryItem.onRetryPending?.();
    expect(retriedId).toBe("submission-rejected");
  });
});

describe("reconcilePendingUserMessagesAgainstEvents", () => {

  test("binds duplicate-text Submissions by causal FIFO identity, never body matching", () => {
    const messages = [
      pending({ id: "submission-a", body: "same", lifecycle: "queued", createdAfterMaxSeq: 10 }),
      pending({ id: "submission-b", body: "same", lifecycle: "queued", createdAfterMaxSeq: 10 }),
    ];
    const reconciled = reconcilePendingUserMessagesAgainstEvents(messages, [
      { id: "provider-echo-a", seq: 11, kind: "user_message", body: "provider-normalized-a" },
      { id: "provider-echo-b", seq: 12, kind: "user_message", body: "provider-normalized-b" },
    ]);

    expect(reconciled.map(({ id, confirmedEventId }) => ({ id, confirmedEventId }))).toEqual([
      { id: "submission-a", confirmedEventId: "provider-echo-a" },
      { id: "submission-b", confirmedEventId: "provider-echo-b" },
    ]);
  });
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

  test("causal FIFO does not require provider text or attachment normalization", () => {
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

  test("reserves attachment-only provider events in causal FIFO order", () => {
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

  test("queue metadata never relocates a Submission behind later server events", () => {
    const merged = mergePendingUserMessagesIntoTimeline(
      [
        { type: "message", id: "before", role: "assistant", timestamp: "2026-07-10T10:00:00.000Z", body: "before", attachments: [] },
        { type: "message", id: "after", role: "assistant", timestamp: "2026-07-10T10:00:02.000Z", body: "after", attachments: [] },
        { type: "activity", id: "working-turn:activity", timestamp: "2026-07-10T10:00:03.000Z", statusKey: "running", title: "Working", tone: "running", icon: "time-outline", defaultExpanded: false },
      ],
      [pendingMessage({
        id: "submission",
        body: "same",
        lifecycle: "queued",
        createdAt: "2026-07-10T10:00:01.000Z",
      })],
    );

    expect(merged.map((item) => item.id)).toEqual([
      "before",
      "submission",
      "after",
      "working-turn:activity",
    ]);
  });
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

  test("equal text with different IDs never collapses two Submissions", () => {
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
    expect(userMessages).toHaveLength(2);
    expect(userMessages.map((item) => item.id)).toEqual([
      "pending-send",
      "echo-1",
    ]);
    expect(userMessages[0]).toMatchObject({ pending: true });
  });

  test("equal attachments with different IDs never collapse two Submissions", () => {
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
    expect(userMessages).toHaveLength(2);
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

describe("canonical terminal-before-ACK reconciliation", () => {
  test("exact IDs preserve duplicate Submission order without turn promotion", () => {
    const pendingMessages = [
      pendingMessage({
        id: "submission-1",
        turnId: "submission-1",
        body: "same",
        lifecycle: "unconfirmed",
        createdAt: "2026-07-10T10:00:00.000Z",
      }),
      pendingMessage({
        id: "submission-2",
        turnId: "submission-2",
        body: "same",
        lifecycle: "unconfirmed",
        createdAt: "2026-07-10T10:00:01.000Z",
      }),
    ];
    const reconciled = reconcilePendingUserMessagesAgainstEvents(
      pendingMessages,
      [
        {
          id: "submission-2",
          position: 3,
          submission_id: "submission-2",
          submission_state: "delivered",
          kind: "user_message",
          body: "same",
        },
        {
          id: "submission-1",
          position: 1,
          submission_id: "submission-1",
          submission_state: "delivered",
          kind: "user_message",
          body: "same",
        },
      ],
    );
    expect(reconciled.map(({ id, confirmedEventId }) => ({ id, confirmedEventId }))).toEqual([
      { id: "submission-1", confirmedEventId: "submission-1" },
      { id: "submission-2", confirmedEventId: "submission-2" },
    ]);

    const timeline: ZenTimelineItem[] = [
      {
        type: "message",
        id: "submission-1",
        role: "user",
        body: "same",
        attachments: [],
      },
      {
        type: "message",
        id: "assistant-between",
        role: "assistant",
        body: "terminal output already arrived",
        attachments: [],
      },
      {
        type: "message",
        id: "submission-2",
        role: "user",
        body: "same",
        attachments: [],
      },
    ];
    const merged = mergePendingUserMessagesIntoTimeline(timeline, reconciled);
    expect(merged.map((item) => item.id)).toEqual([
      "submission-1",
      "assistant-between",
      "submission-2",
    ]);
    expect(merged.some((item) => item.type === "activity" && item.title === "Working")).toBe(false);
  });
});
