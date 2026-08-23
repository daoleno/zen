import { describe, expect, test } from "bun:test";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import {
  brainWorkEventAccessibilityLabel,
  brainWorkEventLifecycle,
  brainWorkEventReviewLabel,
  brainWorkEventSessionLabel,
  brainWorkEventSourceLabel,
  brainWorkEventSummary,
  brainWorkEventWorkTitle,
} from "./brainWorkEventPresentation";

const canonicalSessionID =
  "brain-agent-zen-brain-event-cards-1785779310481592975:@7163";

function resultEvent(
  overrides: Partial<BrainWorkResultEvent> = {},
): BrainWorkResultEvent {
  return {
    event_id: "event-done",
    kind: "session.done",
    work_id: "work-cards",
    work_title: "Ship Brain cards",
    summary: "The delegated implementation completed.",
    session_id: canonicalSessionID,
    session_name: `zen-brain-event-cards (${canonicalSessionID})`,
    occurred_at: "2026-08-04T02:00:00Z",
    unread: true,
    review_state: "queued",
    session_state: "open",
    current_result: true,
    ...overrides,
  };
}

describe("Brain Work event source presentation", () => {
  test("removes Zen's canonical Session identity suffix", () => {
    expect(brainWorkEventSourceLabel(resultEvent())).toBe(
      "zen-brain-event-cards",
    );
  });

  test("removes the canonical Session identity from a legacy Work title", () => {
    const event = resultEvent({
      work_title: `zen-device-revocation-acceptance (${canonicalSessionID})`,
      session_name: "Device revocation worker",
    });

    expect(brainWorkEventWorkTitle(event)).toBe(
      "zen-device-revocation-acceptance",
    );
    const label = brainWorkEventAccessibilityLabel({
      event,
      statusLabel: "Done",
      occurredAtLabel: "August 4, 2026 at 10:00",
    });
    expect(label).toContain("Work zen-device-revocation-acceptance");
    expect(label).not.toContain(canonicalSessionID);
    expect(label).not.toContain("brain-agent-");
  });

  test("omits a normalized source that repeats the Work title", () => {
    expect(
      brainWorkEventSourceLabel(
        resultEvent({
          work_title: "  Zen Work Result Cards  ",
          session_name: `zen work result cards (${canonicalSessionID})`,
        }),
      ),
    ).toBeUndefined();
  });

  test("deduplicates source against the cleaned legacy Work title", () => {
    expect(
      brainWorkEventSourceLabel(
        resultEvent({
          work_title: `zen-device-revocation-acceptance (${canonicalSessionID})`,
          session_name: `ZEN-DEVICE-REVOCATION-ACCEPTANCE (${canonicalSessionID})`,
        }),
      ),
    ).toBeUndefined();
  });

  test("removes implementation Session identities from the visible summary", () => {
    expect(
      brainWorkEventSummary(
        resultEvent({
          summary: `Inspect ${canonicalSessionID} before continuing.`,
        }),
      ),
    ).toBe("Inspect the session before continuing.");
  });

  test("redacts provider Turn identities from visible and accessibility copy", () => {
    const providerTurn = "turn:07012856-dc2a-4430-b18c-ca326706401a";
    const event = resultEvent({ summary: `Review ${providerTurn} next.` });
    expect(brainWorkEventSummary(event)).toBe("Review the provider turn next.");
    expect(
      brainWorkEventAccessibilityLabel({
        event,
        statusLabel: "Ready",
        occurredAtLabel: "August 4, 2026 at 10:00",
      }),
    ).not.toContain(providerTurn);
  });

  test("uses a generic fallback instead of exposing a bare canonical identity", () => {
    expect(
      brainWorkEventSourceLabel(
        resultEvent({ session_name: `(${canonicalSessionID})` }),
      ),
    ).toBeUndefined();
  });

  test("accessibility copy never includes the canonical Session identity", () => {
    const label = brainWorkEventAccessibilityLabel({
      event: resultEvent({
        summary: `The delegated Session ${canonicalSessionID} completed.`,
      }),
      statusLabel: "Done",
      occurredAtLabel: "August 4, 2026 at 10:00",
    });

    expect(label).toContain("Source: zen-brain-event-cards");
    expect(label).not.toContain(canonicalSessionID);
    expect(label).not.toContain("brain-agent-");
  });

  test("keeps result fact, review attention, and Session finalization distinct", () => {
    const queued = resultEvent({
      review_state: "queued",
      session_state: "open",
      current_result: true,
    });
    expect(brainWorkEventReviewLabel(queued)).toBe("Needs review");
    expect(brainWorkEventSessionLabel(queued)).toBeUndefined();

    const reserved = resultEvent({ review_state: "reserved" });
    expect(brainWorkEventReviewLabel(reserved)).toBe(
      "Queued",
    );

    const resolved = resultEvent({
      review_state: "resolved",
      session_state: "finalized",
      current_result: false,
    });
    expect(brainWorkEventReviewLabel(resolved)).toBe("Reviewed");
    expect(brainWorkEventSessionLabel(resolved)).toBeUndefined();
    const label = brainWorkEventAccessibilityLabel({
      event: resolved,
      statusLabel: "Done",
      occurredAtLabel: "August 4, 2026 at 10:00",
    });
    expect(label).toContain("Done");
    expect(label).not.toContain("Reviewed");
    expect(label).not.toContain("Session finalized");
    expect(label).not.toContain("Superseded result");
  });

  test("derives lifecycle from canonical review state before result kind", () => {
    expect(
      brainWorkEventLifecycle(resultEvent({ review_state: "queued" })).label,
    ).toBe("Ready");
    expect(
      brainWorkEventLifecycle(resultEvent({ review_state: "reserved" })).label,
    ).toBe("Ready");
    expect(
      brainWorkEventLifecycle(resultEvent({ review_state: "reviewing" })).label,
    ).toBe("Reviewing");
    expect(
      brainWorkEventLifecycle(resultEvent({ review_state: "resolved" })).label,
    ).toBe("Done");
  });

  test("keeps needs-user attention distinct from terminal failure", () => {
    expect(
      brainWorkEventLifecycle(resultEvent({ kind: "session.needs_input" })),
    ).toMatchObject({ label: "Needs you", tone: "attention", terminal: false });
    expect(
      brainWorkEventLifecycle(resultEvent({ kind: "session.failed" })),
    ).toMatchObject({ label: "Failed", tone: "danger", terminal: true });
    expect(
      brainWorkEventLifecycle(
        resultEvent({
          kind: "session.needs_input",
          review_state: "resolved",
        }),
      ).label,
    ).toBe("Done");
  });
});
