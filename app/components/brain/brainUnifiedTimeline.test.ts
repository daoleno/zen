import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../../services/codexConversation";
import {
  attachBrainWorkEventActions,
  buildZenTimeline,
} from "../terminal/InterfaceTimelineModel";

describe("Brain work cards in canonical conversation timeline", () => {
  test("renders work_result status events as brain-work-event items", () => {
    const event: CodexConversationEvent = {
      id: "event-1",
      seq: 1,
      timestamp: "2026-08-06T10:00:00.000Z",
      kind: "status",
      title: "Delegated work",
      body: "Needs a decision",
      status: "session.needs_input",
      source: "work_result",
      work_id: "work-1",
      work_session_id: "sess-1",
      session_name: "agent",
      unread: true,
    };
    const items = buildZenTimeline([
      {
        id: "user-1",
        seq: 0,
        timestamp: "2026-08-06T09:59:00.000Z",
        kind: "user_message",
        role: "user",
        body: "hello",
      },
      event,
    ]);
    expect(items.map((item) => item.type)).toEqual([
      "message",
      "brain-work-event",
    ]);
    const card = items[1];
    if (card.type !== "brain-work-event") {
      throw new Error("expected work card");
    }
    expect(card.event).toEqual({
      event_id: "event-1",
      kind: "session.needs_input",
      work_id: "work-1",
      work_title: "Delegated work",
      summary: "Needs a decision",
      session_id: "sess-1",
      session_name: "agent",
      occurred_at: "2026-08-06T10:00:00.000Z",
      unread: true,
    });
  });

  test("attaches activate handlers for unread or openable cards", () => {
    const items = buildZenTimeline([
      {
        id: "event-1",
        seq: 1,
        timestamp: "2026-08-06T10:00:00.000Z",
        kind: "status",
        title: "Work",
        body: "Done",
        status: "session.done",
        source: "work_result",
        work_id: "work-1",
        work_session_id: "sess-1",
        unread: true,
      },
    ]);
    const activated: string[] = [];
    const enriched = attachBrainWorkEventActions(
      items,
      (event) => {
        activated.push(event.event_id);
      },
      new Set(["sess-1"]),
    );
    const card = enriched[0];
    if (card.type !== "brain-work-event" || !card.onPress) {
      throw new Error("expected actionable work card");
    }
    card.onPress();
    expect(activated).toEqual(["event-1"]);
  });
});
