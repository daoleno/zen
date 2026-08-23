import { describe, expect, test } from "bun:test";
import type { BrainCurrentWork } from "../../store/brain";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import {
  buildBrainWorkActivityModel,
  groupBrainWorkEvents,
} from "./brainWorkActivityModel";

const canonicalSessionId = "brain-agent-private-1787466395690315716:@602";

function resultEvent(
  overrides: Partial<BrainWorkResultEvent> = {},
): BrainWorkResultEvent {
  return {
    event_id: "event-1",
    kind: "session.done",
    work_id: "work-1",
    work_title: "Ship Work activity",
    summary: "Implementation is ready for Brain review.",
    session_id: canonicalSessionId,
    session_name: `worker (${canonicalSessionId})`,
    occurred_at: "2026-08-23T06:00:00.000Z",
    unread: true,
    review_state: "queued",
    session_state: "open",
    current_result: true,
    ...overrides,
  };
}

function currentWork(
  overrides: Partial<BrainCurrentWork> = {},
): BrainCurrentWork {
  return {
    work_id: "work-1",
    revision: 1,
    title: "Ship Work activity",
    status: "running",
    progress_mode: "owned",
    attempt_session_id: canonicalSessionId,
    attempt_delegated: true,
    unread_result: false,
    ...overrides,
  };
}

describe("Brain Work activity model", () => {
  test("groups result revisions by work_id and preserves first-seen identity", () => {
    const groups = groupBrainWorkEvents([
      resultEvent(),
      resultEvent({
        event_id: "event-reviewing",
        review_state: "reviewing",
        current_result: true,
      }),
      resultEvent({
        event_id: "event-superseded",
        review_state: "resolved",
        current_result: false,
      }),
      resultEvent({
        event_id: "event-other",
        work_id: "work-2",
        work_title: "Second Work",
      }),
    ]);

    expect(groups.map((group) => group.workId)).toEqual(["work-1", "work-2"]);
    expect(groups[0]?.events).toHaveLength(3);
    expect(groups[0]?.currentEvent.event_id).toBe("event-reviewing");
  });

  test("counts simultaneous active and genuine needs-user Work once each", () => {
    const model = buildBrainWorkActivityModel({
      currentWork: [
        currentWork(),
        currentWork({
          work_id: "work-2",
          title: "Review Work",
          attention_state: "reviewing",
        }),
        currentWork({
          work_id: "work-3",
          title: "Answer Work",
          status: "needs_input",
        }),
      ],
      resultEvents: [
        resultEvent(),
        resultEvent({
          event_id: "event-2",
          work_id: "work-2",
          work_title: "Review Work",
          review_state: "reviewing",
        }),
        resultEvent({
          event_id: "event-3",
          work_id: "work-3",
          work_title: "Answer Work",
          kind: "session.needs_input",
        }),
      ],
      openSessionIds: new Set([canonicalSessionId]),
    });

    expect(model.activeCount).toBe(3);
    expect(model.attentionCount).toBe(1);
    expect(model.active.map((row) => row.presentation.label)).toEqual([
      "Ready",
      "Reviewing",
      "Needs you",
    ]);
    expect(model.accessibilityLabel).toContain("3 active");
    expect(model.accessibilityLabel).toContain("1 need you");
  });

  test("places only resolved and failed results in History", () => {
    const model = buildBrainWorkActivityModel({
      currentWork: [],
      resultEvents: [
        resultEvent({ work_id: "ready", review_state: "reserved" }),
        resultEvent({ work_id: "reviewing", review_state: "reviewing" }),
        resultEvent({ work_id: "done", review_state: "resolved" }),
        resultEvent({ work_id: "failed", kind: "session.failed" }),
      ],
      openSessionIds: new Set(),
    });

    expect(model.active.map((row) => row.presentation.label)).toEqual([
      "Ready",
      "Reviewing",
    ]);
    expect(model.history.map((row) => row.presentation.label)).toEqual([
      "Done",
      "Failed",
    ]);
  });

  test("redacts canonical Session IDs and preserves activation authority", () => {
    const event = resultEvent({
      summary: `Open ${canonicalSessionId} to continue.`,
    });
    const model = buildBrainWorkActivityModel({
      currentWork: [],
      resultEvents: [event],
      openSessionIds: new Set([canonicalSessionId]),
    });
    const row = model.active[0];

    expect(row?.summary).toBe("Open the session to continue.");
    expect(
      JSON.stringify({
        title: row?.title,
        summary: row?.summary,
        accessibilityLabel: model.accessibilityLabel,
      }),
    ).not.toContain(canonicalSessionId);
    expect(row?.event).toBe(event);
    expect(row?.canOpenSession).toBe(true);
  });

  test("counts delegated sources only from distinct canonical Session events", () => {
    const model = buildBrainWorkActivityModel({
      currentWork: [],
      resultEvents: [
        resultEvent(),
        resultEvent({ event_id: "same-source" }),
        resultEvent({
          event_id: "second-source",
          session_id: "brain-agent-second-1:@9",
        }),
      ],
      openSessionIds: new Set(),
    });

    expect(model.active[0]?.sourceCount).toBe(2);
  });
});
