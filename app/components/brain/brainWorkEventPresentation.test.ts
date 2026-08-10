import { describe, expect, test } from "bun:test";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import {
  brainWorkEventAccessibilityLabel,
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
      statusLabel: "Completed",
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

  test("replaces canonical Session identities in the visible summary only", () => {
    expect(
      brainWorkEventSummary(
        resultEvent({
          summary: `Inspect ${canonicalSessionID} before continuing.`,
        }),
      ),
    ).toBe("Inspect Delegated Session before continuing.");
  });

  test("uses a generic fallback instead of exposing a bare canonical identity", () => {
    expect(
      brainWorkEventSourceLabel(
        resultEvent({ session_name: `(${canonicalSessionID})` }),
      ),
    ).toBe("Delegated Session");
  });

  test("accessibility copy never includes the canonical Session identity", () => {
    const label = brainWorkEventAccessibilityLabel({
      event: resultEvent({
        summary: `The delegated Session ${canonicalSessionID} completed.`,
      }),
      statusLabel: "Completed",
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
    expect(brainWorkEventReviewLabel(queued)).toBe("Queued for Brain review");
    expect(brainWorkEventSessionLabel(queued)).toBe("Session open");

    const reserved = resultEvent({ review_state: "reserved" });
    expect(brainWorkEventReviewLabel(reserved)).toBe(
      "Reserved for next Brain turn",
    );

    const resolved = resultEvent({
      review_state: "resolved",
      session_state: "finalized",
      current_result: false,
    });
    expect(brainWorkEventReviewLabel(resolved)).toBe("Brain resolved");
    expect(brainWorkEventSessionLabel(resolved)).toBe("Session finalized");
    const label = brainWorkEventAccessibilityLabel({
      event: resolved,
      statusLabel: "Completed",
      occurredAtLabel: "August 4, 2026 at 10:00",
    });
    expect(label).toContain("Completed");
    expect(label).toContain("Brain resolved");
    expect(label).toContain("Session finalized");
    expect(label).toContain("Superseded result");
  });
});
