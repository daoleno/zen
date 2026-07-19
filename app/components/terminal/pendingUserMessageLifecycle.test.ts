import { describe, expect, test } from "bun:test";
import type { PendingUserMessage } from "./InterfaceChatSession";
import {
  beginPendingUserMessageAttempt,
  presentPendingUserMessageLifecycle,
  reconcilePendingUserMessagesAgainstEvents,
  rejectPendingUserMessage,
} from "./pendingUserMessageLifecycle";
import {
  buildZenTimeline,
  mergePendingUserMessagesIntoTimeline,
} from "./InterfaceTimelineModel";

function pending(
  overrides: Partial<PendingUserMessage> = {},
): PendingUserMessage {
  return {
    id: "pending-a",
    body: "hello",
    sentText: "hello",
    attachments: [],
    createdAt: "2026-07-17T10:00:00.000Z",
    lifecycle: "pending",
    dispatchRequestId: "request-a",
    dispatchAttemptOrder: 1,
    createdAfterMaxSeq: 10,
    createdAfterEventIds: ["history"],
    ...overrides,
  };
}

describe("pending user message lifecycle", () => {
  test("the local row has only pending and failed presentations", () => {
    expect(presentPendingUserMessageLifecycle(pending())).toEqual({
      lifecycle: "pending",
      label: "Pending",
      accessibilityLabel: "Message pending provider transcript",
    });
    expect(
      presentPendingUserMessageLifecycle(pending({ lifecycle: "failed" })),
    ).toEqual({
      lifecycle: "failed",
      label: "Send failed",
      accessibilityLabel: "Message send failed",
    });
  });

  test("manual retry keeps one UI row but starts a fresh request attempt", () => {
    const failed = pending({
      lifecycle: "failed",
      failureCode: "send_input_failed",
      failureMessage: "provider unavailable",
    });
    const retried = beginPendingUserMessageAttempt(failed, {
      requestId: "request-b",
      dispatchAttemptOrder: 2,
      createdAfterMaxSeq: 20,
      createdAfterEventIds: ["history", "provider-between"],
    });
    expect(retried).toEqual({
      ...failed,
      lifecycle: "pending",
      dispatchRequestId: "request-b",
      dispatchAttemptOrder: 2,
      failureCode: undefined,
      failureMessage: undefined,
      createdAfterMaxSeq: 20,
      createdAfterEventIds: ["history", "provider-between"],
    });
    expect(retried.id).toBe(failed.id);
    expect(retried.createdAt).toBe(failed.createdAt);
  });

  test("only the latest attempt's correlated failure can fail a row", () => {
    const current = pending({ dispatchRequestId: "request-current" });
    const stale = rejectPendingUserMessage(current, {
      requestId: "request-previous",
      code: "old_failure",
      message: "old attempt",
    });
    expect(stale).toBe(current);

    expect(
      rejectPendingUserMessage(current, {
        requestId: "request-current",
        code: "send_input_failed",
        message: "provider refused input",
      }),
    ).toMatchObject({
      id: current.id,
      lifecycle: "failed",
      dispatchRequestId: "request-current",
      failureCode: "send_input_failed",
      failureMessage: "provider refused input",
    });
  });
});

describe("provider transcript reconciliation", () => {
  test("retry reconciliation follows dispatch attempts without reordering UI rows", () => {
    const failedA = pending({
      id: "pending-a",
      lifecycle: "failed",
      dispatchAttemptOrder: 1,
    });
    const pendingB = pending({
      id: "pending-b",
      dispatchRequestId: "request-b",
      dispatchAttemptOrder: 2,
    });
    const retriedA = beginPendingUserMessageAttempt(failedA, {
      requestId: "request-a-retry",
      dispatchAttemptOrder: 3,
      createdAfterMaxSeq: 10,
      createdAfterEventIds: ["history"],
    });
    const localPresentationOrder = [retriedA, pendingB];
    expect(localPresentationOrder.map((message) => message.id)).toEqual([
      "pending-a",
      "pending-b",
    ]);

    const providerB = {
      id: "provider-b",
      seq: 11,
      kind: "user_message",
    };
    const providerA = {
      id: "provider-a-retry",
      seq: 12,
      kind: "user_message",
    };
    const combined = reconcilePendingUserMessagesAgainstEvents(
      localPresentationOrder,
      [{ id: "history", seq: 10, kind: "user_message" }, providerB, providerA],
    );
    expect(combined.pendingUserMessages).toEqual([]);
    expect(combined.providerEventAliases).toEqual([
      { providerEventId: "provider-b", localPendingId: "pending-b" },
      { providerEventId: "provider-a-retry", localPendingId: "pending-a" },
    ]);

    const firstEcho = reconcilePendingUserMessagesAgainstEvents(
      localPresentationOrder,
      [{ id: "history", seq: 10, kind: "user_message" }, providerB],
    );
    expect(firstEcho.pendingUserMessages.map((message) => message.id)).toEqual([
      "pending-a",
    ]);
    expect(firstEcho.providerEventAliases).toEqual([
      { providerEventId: "provider-b", localPendingId: "pending-b" },
    ]);

    const secondEcho = reconcilePendingUserMessagesAgainstEvents(
      firstEcho.pendingUserMessages,
      [{ id: "history", seq: 10, kind: "user_message" }, providerB, providerA],
    );
    expect(secondEcho.pendingUserMessages).toEqual([]);
    expect(secondEcho.providerEventAliases).toEqual([
      { providerEventId: "provider-a-retry", localPendingId: "pending-a" },
    ]);
  });

  test("causal reconciliation projects provider ids onto local turn anchors", () => {
    const rows = [
      pending({ id: "pending-a" }),
      pending({ id: "pending-b", dispatchRequestId: "request-b" }),
    ];
    const reconciled = reconcilePendingUserMessagesAgainstEvents(rows, [
      { id: "history", seq: 10, kind: "user_message" },
      { id: "provider-echo-a", seq: 11, kind: "user_message" },
      { id: "provider-echo-b", seq: 12, kind: "user_message" },
    ]);

    expect(reconciled.pendingUserMessages).toEqual([]);
    expect(reconciled.providerEventAliases).toEqual([
      { providerEventId: "provider-echo-a", localPendingId: "pending-a" },
      { providerEventId: "provider-echo-b", localPendingId: "pending-b" },
    ]);
  });

  test("provider user events consume local rows in causal FIFO order", () => {
    const rows = [
      pending({ id: "first" }),
      pending({ id: "second", dispatchRequestId: "request-b" }),
    ];
    expect(
      reconcilePendingUserMessagesAgainstEvents(rows, [
        { id: "history", seq: 10, kind: "user_message" },
        { id: "provider-echo-a", seq: 11, kind: "user_message" },
        { id: "provider-echo-b", seq: 12, kind: "user_message" },
      ]).pendingUserMessages,
    ).toEqual([]);
  });

  test("an echo consumed by an earlier row cannot consume the next row later", () => {
    const rows = [
      pending({ id: "first" }),
      pending({ id: "second", dispatchRequestId: "request-b" }),
    ];
    const afterFirstEcho = reconcilePendingUserMessagesAgainstEvents(rows, [
      { id: "history", seq: 10, kind: "user_message" },
      { id: "provider-echo-a", seq: 11, kind: "user_message" },
    ]).pendingUserMessages;
    expect(afterFirstEcho).toHaveLength(1);
    expect(afterFirstEcho[0]).toMatchObject({
      id: "second",
      createdAfterEventIds: ["history", "provider-echo-a"],
    });

    expect(
      reconcilePendingUserMessagesAgainstEvents(afterFirstEcho, [
        { id: "history", seq: 10, kind: "user_message" },
        { id: "provider-echo-a", seq: 11, kind: "user_message" },
      ]).pendingUserMessages,
    ).toHaveLength(1);
    expect(
      reconcilePendingUserMessagesAgainstEvents(afterFirstEcho, [
        { id: "history", seq: 10, kind: "user_message" },
        { id: "provider-echo-a", seq: 11, kind: "user_message" },
        { id: "provider-echo-b", seq: 12, kind: "user_message" },
      ]).pendingUserMessages,
    ).toEqual([]);
  });

  test("events at or before the send boundary never consume a local row", () => {
    const row = pending({
      createdAfterMaxSeq: 20,
      createdAfterEventIds: ["known-by-id"],
    });
    expect(
      reconcilePendingUserMessagesAgainstEvents(
        [row],
        [
          { id: "known-by-id", seq: 21, kind: "user_message" },
          { id: "old-by-seq", seq: 20, kind: "user_message" },
        ],
      ).pendingUserMessages,
    ).toEqual([row]);
  });

  test("message bodies never participate in provider echo identity", () => {
    const row = pending({ body: "same body", sentText: "same body" });
    const oldEventWithSameBody = {
      id: "old-by-seq",
      seq: 10,
      kind: "user_message",
      body: "same body",
    };
    expect(
      reconcilePendingUserMessagesAgainstEvents([row], [oldEventWithSameBody])
        .pendingUserMessages,
    ).toEqual([row]);

    const futureEventWithDifferentBody = {
      id: "provider-echo",
      seq: 11,
      kind: "user_message",
      body: "different body",
    };
    expect(
      reconcilePendingUserMessagesAgainstEvents(
        [row],
        [futureEventWithDifferentBody],
      ).pendingUserMessages,
    ).toEqual([]);
  });

  test("provider transcript wins even if the matching local attempt was marked failed", () => {
    const row = pending({ lifecycle: "failed" });
    expect(
      reconcilePendingUserMessagesAgainstEvents(
        [row],
        [{ id: "provider-echo", seq: 11, kind: "user_message" }],
      ).pendingUserMessages,
    ).toEqual([]);
  });
});

describe("pending timeline rows", () => {
  test("local rows render in FIFO order at their causal boundary", () => {
    const timeline = buildZenTimeline([
      {
        id: "history",
        seq: 10,
        timestamp: "2026-07-17T09:59:00.000Z",
        kind: "assistant_message",
        body: "history",
      },
    ]);
    const merged = mergePendingUserMessagesIntoTimeline(timeline, [
      pending({ id: "first" }),
      pending({
        id: "second",
        lifecycle: "failed",
        dispatchRequestId: "request-b",
        failureMessage: "provider refused input",
      }),
    ]);
    expect(merged.map((item) => item.id)).toEqual([
      "history",
      "first",
      "second",
    ]);
    expect(merged[1]).toMatchObject({
      pending: true,
      pendingLifecycle: "pending",
      pendingLifecycleLabel: "Pending",
    });
    expect(merged[2]).toMatchObject({
      pending: true,
      pendingLifecycle: "failed",
      pendingLifecycleLabel: "Send failed",
      pendingFailureMessage: "provider refused input",
    });
  });

  test("Retry is attached only to failed local rows", () => {
    const retried: string[] = [];
    const merged = mergePendingUserMessagesIntoTimeline(
      [],
      [
        pending({ id: "pending" }),
        pending({ id: "failed", lifecycle: "failed" }),
      ],
      (id) => retried.push(id),
    );
    expect(
      merged[0]?.type === "message" && merged[0].onRetryPending,
    ).toBeUndefined();
    if (merged[1]?.type !== "message" || !merged[1].onRetryPending) {
      throw new Error("expected failed row retry action");
    }
    merged[1].onRetryPending();
    expect(retried).toEqual(["failed"]);
  });

  test("provider rows never inherit App-local pending lifecycle", () => {
    const timeline = buildZenTimeline([
      {
        id: "provider-user",
        seq: 1,
        kind: "user_message",
        body: "provider truth",
      },
    ]);
    expect(timeline).toHaveLength(1);
    expect(timeline[0]).toMatchObject({
      id: "provider-user",
      role: "user",
    });
    expect(timeline[0]?.type === "message" && timeline[0].pending).toBeFalsy();
  });
});
