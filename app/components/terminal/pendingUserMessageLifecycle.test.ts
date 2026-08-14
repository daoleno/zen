import { describe, expect, test } from "bun:test";
import type { PendingUserMessage } from "./InterfaceChatSession";
import {
  beginPendingUserMessageAttempt,
  presentPendingUserMessageLifecycle,
  reconcilePendingUserMessagesAgainstEvents,
  rejectPendingUserMessage,
  showsPendingSendStatusMark,
  staleReceiptAutoRetryPolicy,
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
  test("pending presentation is mark/a11y only; failed keeps visible status", () => {
    expect(presentPendingUserMessageLifecycle(pending())).toEqual({
      lifecycle: "pending",
      label: "",
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

  test("pending send mark is shown only for in-flight transport", () => {
    expect(
      showsPendingSendStatusMark({ pending: true, lifecycle: "pending" }),
    ).toBe(true);
    expect(
      showsPendingSendStatusMark({ pending: true, lifecycle: "failed" }),
    ).toBe(false);
    expect(showsPendingSendStatusMark({ pending: false })).toBe(false);
    expect(showsPendingSendStatusMark({})).toBe(false);
  });

  test("user bubble drops Pending border and hosts an external send mark", async () => {
    const bubbleSource = await Bun.file(
      new URL("./InterfaceTimelineMessage.tsx", import.meta.url),
    ).text();
    const footerSource = await Bun.file(
      new URL("./MessageBubbleFooter.tsx", import.meta.url),
    ).text();
    const markSource = await Bun.file(
      new URL("./PendingSendStatusMark.tsx", import.meta.url),
    ).text();
    const listSource = await Bun.file(
      new URL("./InterfaceTimelineView.tsx", import.meta.url),
    ).text();
    const zenUser = bubbleSource.slice(
      bubbleSource.indexOf("export function ZenUserMessage"),
      bubbleSource.indexOf("function HeartbeatWakeCard"),
    );
    expect(zenUser).not.toContain("resolvePendingUserBubbleBorderColor");
    expect(zenUser).not.toContain("StyleSheet.hairlineWidth");
    expect(zenUser).not.toContain("borderColor:");
    expect(zenUser).not.toContain("borderWidth:");
    expect(zenUser).toContain("showsPendingSendStatusMark({");
    expect(zenUser).toContain("<PendingSendStatusMark");
    expect(zenUser).toMatch(
      /PendingSendStatusMark color=\{zenTheme\.chat\.outboundSentClock\}/,
    );
    expect(zenUser).toContain("styles.pendingSendMark");
    expect(zenUser).not.toContain("userBubbleHost");
    expect(bubbleSource).not.toContain("userBubbleHost");
    expect(bubbleSource).toContain(
      "right: PENDING_SEND_STATUS_OUTSIDE_RIGHT",
    );
    expect(bubbleSource).toMatch(/userBubble:[\s\S]*?maxWidth: "86%"/);
    expect(bubbleSource).toMatch(/userBubbleChatGpt:[\s\S]*?maxWidth: "88%"/);
    expect(bubbleSource).toMatch(/pendingSendMark:[\s\S]*bottom: 0/);
    expect(zenUser).toContain(
      "accessibilityState={item.pending ? { busy: true } : undefined}",
    );
    expect(zenUser).not.toMatch(/accessibilityLabel\s*=/);
    expect(zenUser).not.toContain("lifecycleAccessibilityLabel");
    expect(zenUser).not.toMatch(/["']Pending["']/);
    expect(zenUser).not.toMatch(/["']Sending["']/);
    expect(footerSource).not.toContain("pending?:");
    expect(footerSource).not.toContain("lifecycleAccessibilityLabel");
    expect(markSource).toContain("useReducedMotion");
    expect(markSource).toContain("withRepeat");
    expect(markSource).toContain("ReduceMotion.System");
    expect(markSource).toContain("PENDING_SEND_CLOCK_FAST_HAND_REST_DEG");
    expect(markSource).toContain("PENDING_SEND_CLOCK_SLOW_HAND_REST_DEG");
    expect(markSource).toContain("PENDING_SEND_CLOCK_FAST_PERIOD_MS");
    expect(markSource).toContain("PENDING_SEND_CLOCK_SLOW_PERIOD_MS");
    expect(markSource).toContain("bottom: center");
    expect(markSource).toContain("PENDING_SEND_CLOCK_HAND_STROKE / 2");
    expect(markSource).not.toContain("marginBottom");
    expect(markSource).not.toContain("setInterval");
    expect(markSource).not.toContain("setTimeout");
    expect(markSource).not.toContain("<Line");
    // Presentation keys stay canonical event ids; turnFocusAnchorId is
    // turn-focus only and must not become FlatList identity.
    expect(listSource).toContain("keyExtractor={(item) => item.id}");
    expect(listSource).not.toContain("turnFocusAnchorId ??");
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
  test("exact receipt match clears Pending before causal FIFO", () => {
    // Live duplicate: durable Brain admission id === dispatchRequestId, but
    // seq is inside the send boundary so FIFO alone would leave the optimistic
    // row on screen beside the canonical event.
    const receiptId = "msh1e2ak_atzbs1";
    const local = pending({
      id: "local-optimistic",
      body: "canonical admitted body",
      sentText: "canonical admitted body",
      dispatchRequestId: receiptId,
      createdAfterMaxSeq: 99,
      createdAfterEventIds: ["history"],
    });
    const admission = {
      id: receiptId,
      seq: 5,
      kind: "user_message",
    };
    const reconciled = reconcilePendingUserMessagesAgainstEvents(
      [local],
      [{ id: "history", seq: 1, kind: "user_message" }, admission],
    );
    expect(reconciled.pendingUserMessages).toEqual([]);
    expect(reconciled.providerEventAliases).toEqual([
      { providerEventId: receiptId, localPendingId: "local-optimistic" },
    ]);
  });

  test("exact receipt matching is global across concurrent out-of-order admissions", () => {
    const rows = [
      pending({
        id: "local-a",
        dispatchRequestId: "receipt-a",
        dispatchAttemptOrder: 1,
        createdAfterMaxSeq: 50,
      }),
      pending({
        id: "local-b",
        dispatchRequestId: "receipt-b",
        dispatchAttemptOrder: 2,
        createdAfterMaxSeq: 50,
      }),
    ];
    // Snapshot delivers B before A; both seqs would be blocked by FIFO bounds.
    const reconciled = reconcilePendingUserMessagesAgainstEvents(rows, [
      { id: "receipt-b", seq: 3, kind: "user_message" },
      { id: "receipt-a", seq: 2, kind: "user_message" },
    ]);
    expect(reconciled.pendingUserMessages).toEqual([]);
    expect(reconciled.providerEventAliases).toEqual([
      { providerEventId: "receipt-a", localPendingId: "local-a" },
      { providerEventId: "receipt-b", localPendingId: "local-b" },
    ]);
  });

  test("retry exact-matches the new receipt; prior admission id does not steal it", () => {
    const failed = pending({
      id: "local-row",
      lifecycle: "failed",
      dispatchRequestId: "receipt-old",
      dispatchAttemptOrder: 1,
      createdAfterMaxSeq: 10,
      createdAfterEventIds: ["history", "receipt-old"],
    });
    const retried = beginPendingUserMessageAttempt(failed, {
      requestId: "receipt-new",
      dispatchAttemptOrder: 2,
      createdAfterMaxSeq: 10,
      createdAfterEventIds: ["history", "receipt-old"],
    });
    const mid = reconcilePendingUserMessagesAgainstEvents([retried], [
      { id: "history", seq: 1, kind: "user_message" },
      { id: "receipt-old", seq: 2, kind: "user_message" },
    ]);
    expect(mid.pendingUserMessages).toHaveLength(1);
    expect(mid.pendingUserMessages[0]?.dispatchRequestId).toBe("receipt-new");

    const done = reconcilePendingUserMessagesAgainstEvents(
      mid.pendingUserMessages,
      [
        { id: "history", seq: 1, kind: "user_message" },
        { id: "receipt-old", seq: 2, kind: "user_message" },
        { id: "receipt-new", seq: 3, kind: "user_message" },
      ],
    );
    expect(done.pendingUserMessages).toEqual([]);
    expect(done.providerEventAliases).toEqual([
      { providerEventId: "receipt-new", localPendingId: "local-row" },
    ]);
  });

  test("provider echo with a different id still clears via causal FIFO after exact pass", () => {
    const local = pending({
      id: "local-optimistic",
      dispatchRequestId: "client-request-1",
      createdAfterMaxSeq: 10,
    });
    const reconciled = reconcilePendingUserMessagesAgainstEvents([local], [
      { id: "history", seq: 10, kind: "user_message" },
      { id: "provider-echo-xyz", seq: 11, kind: "user_message" },
    ]);
    expect(reconciled.pendingUserMessages).toEqual([]);
    expect(reconciled.providerEventAliases).toEqual([
      {
        providerEventId: "provider-echo-xyz",
        localPendingId: "local-optimistic",
      },
    ]);
  });

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

  test("events at or before the send boundary never consume a local row via FIFO", () => {
    const row = pending({
      dispatchRequestId: "unrelated-request",
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
  test("accepted receipt yields exactly one rendered user row without Pending text", () => {
    const receiptId = "msh1e2ak_atzbs1";
    const body = "live admitted chinese body";
    const local = pending({
      id: "local-optimistic",
      body,
      sentText: body,
      dispatchRequestId: receiptId,
      createdAfterMaxSeq: 99,
    });
    const reconciled = reconcilePendingUserMessagesAgainstEvents([local], [
      { id: receiptId, seq: 2, kind: "user_message" },
    ]);
    expect(reconciled.pendingUserMessages).toEqual([]);

    const timeline = buildZenTimeline([
      {
        id: receiptId,
        seq: 2,
        timestamp: "2026-08-06T04:49:00.000Z",
        kind: "user_message",
        body,
      },
    ]);
    const merged = mergePendingUserMessagesIntoTimeline(
      timeline,
      reconciled.pendingUserMessages,
    );
    const userRows = merged.filter(
      (item) => item.type === "message" && item.role === "user",
    );
    expect(userRows).toHaveLength(1);
    expect(userRows[0]).toMatchObject({
      id: receiptId,
      body,
    });
    expect(userRows[0]?.type === "message" && userRows[0].pending).toBeFalsy();
    expect(
      userRows[0]?.type === "message" && userRows[0].pendingLifecycleLabel,
    ).toBeFalsy();
    expect(JSON.stringify(merged)).not.toContain("Pending");
    expect(JSON.stringify(merged)).not.toContain("Sending");
  });

  test("local rows render in FIFO order at their causal boundary without Pending label", () => {
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
    });
    expect(
      merged[1]?.type === "message" && merged[1].pendingLifecycleLabel,
    ).toBeUndefined();
    expect(JSON.stringify(merged[1])).not.toContain("Pending");
    expect(JSON.stringify(merged[1])).not.toContain("Sending");
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

describe("stale receipt recovery", () => {
  test("stale receipt failure auto-retries exactly once with a fresh identity", () => {
    expect(
      staleReceiptAutoRetryPolicy(pending(), {
        code: "stale_receipt_invalidated",
      }),
    ).toEqual({ autoRetry: true });
    const retried = beginPendingUserMessageAttempt(
      pending({ lifecycle: "failed", failureCode: "stale_receipt_invalidated" }),
      {
        requestId: "request-fresh",
        dispatchAttemptOrder: 2,
        staleReceiptAutoRetried: true,
      },
    );
    expect(retried).toMatchObject({
      lifecycle: "pending",
      dispatchRequestId: "request-fresh",
      staleReceiptAutoRetried: true,
    });
    expect(
      staleReceiptAutoRetryPolicy(retried, {
        code: "stale_receipt_invalidated",
      }),
    ).toEqual({ autoRetry: false, reason: "already_retried" });
  });

  test("ordinary failures never trigger the stale auto-retry", () => {
    expect(
      staleReceiptAutoRetryPolicy(pending(), { code: "send_input_failed" }),
    ).toEqual({ autoRetry: false, reason: "not_stale" });
  });

  test("manual retries preserve the auto-retry bound across attempts", () => {
    const bounded = pending({
      staleReceiptAutoRetried: true,
      dispatchRequestId: "request-fresh",
    });
    const manualRetry = beginPendingUserMessageAttempt(bounded, {
      requestId: "request-fresh",
      dispatchAttemptOrder: 3,
    });
    expect(manualRetry.staleReceiptAutoRetried).toBe(true);
    expect(
      staleReceiptAutoRetryPolicy(manualRetry, {
        code: "stale_receipt_invalidated",
      }),
    ).toEqual({ autoRetry: false, reason: "already_retried" });
  });
});
