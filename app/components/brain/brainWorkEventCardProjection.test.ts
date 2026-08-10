import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../../services/codexConversation";
import {
  attachBrainWorkEventActions,
  buildZenTimeline,
} from "../terminal/InterfaceTimelineModel";
import { projectZenTimeline } from "../terminal/projectZenTimeline";
import type { ZenTimelineItem } from "../terminal/InterfaceTimelineItemView";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const PROJECTED_KINDS = [
  "session.done",
  "session.failed",
  "session.needs_input",
  "session.stale",
  "session.uncertain",
] as const;

function workResultEvent(
  kind: (typeof PROJECTED_KINDS)[number],
  id: string,
): CodexConversationEvent {
  return {
    id,
    seq: 1,
    timestamp: "2026-08-06T10:19:08.365Z",
    kind: "status",
    title: "zen-telegram-performance-publish",
    body: "Delegated provider process or pane is no longer live",
    status: kind,
    source: "work_result",
    work_id: "ae621005-929b-49b5-9d42-fa476d42d3f3",
    work_session_id:
      "brain-agent-zen-telegram-performance-publish-1786011456826849565:@7730",
    session_name:
      "zen-telegram-performance-publish (brain-agent-zen-telegram-performance-publish-1786011456826849565:@7730)",
    unread: true,
    work_review_state: "queued",
    work_session_state: "open",
    work_result_current: true,
  };
}

function assertBrainWorkEventCard(
  items: ZenTimelineItem[],
  kind: (typeof PROJECTED_KINDS)[number],
  id: string,
) {
  expect(items.map((item) => item.type)).toEqual(["brain-work-event"]);
  const card = items[0];
  if (card.type !== "brain-work-event") {
    throw new Error("expected brain-work-event");
  }
  expect(card.id).toBe(id);
  expect(card.event.kind).toBe(kind);
  expect(card.event.work_id).toBe("ae621005-929b-49b5-9d42-fa476d42d3f3");
  expect(JSON.stringify(card)).not.toContain("zen_work_event");
}

describe("Brain Work Event dedicated card projection", () => {
  test("real-time work_result status kinds become brain-work-event cards", () => {
    for (const kind of PROJECTED_KINDS) {
      const event = workResultEvent(kind, `live-${kind}`);
      assertBrainWorkEventCard(buildZenTimeline([event]), kind, event.id);
    }
  });

  test("history replay and incremental projection preserve BrainWorkEventCard items", () => {
    const history = PROJECTED_KINDS.map((kind, index) =>
      workResultEvent(kind, `history-${kind}-${index}`),
    );
    const initial = projectZenTimeline(history, null);
    expect(initial.items.map((item) => item.type)).toEqual([
      "brain-work-event",
      "brain-work-event",
      "brain-work-event",
      "brain-work-event",
      "brain-work-event",
    ]);
    for (const item of initial.items) {
      if (item.type !== "brain-work-event") {
        throw new Error("expected work card");
      }
      expect(item.event.kind).toMatch(/^session\.(done|failed|needs_input|stale|uncertain)$/);
    }

    const withAssistant: CodexConversationEvent[] = [
      ...history,
      {
        id: "assistant-followup",
        seq: 99,
        timestamp: "2026-08-06T10:20:00.000Z",
        kind: "assistant_message",
        role: "assistant",
        body: "Inspecting the failure",
      },
    ];
    const next = projectZenTimeline(withAssistant, initial.cache);
    expect(next.mode).toBe("incremental");
    for (let index = 0; index < initial.items.length; index += 1) {
      expect(next.items[index]).toBe(initial.items[index]);
    }
    expect(next.items[next.items.length - 1]?.type).toBe("message");
  });

  test("attachBrainWorkEventActions never downgrades semantic card kind", () => {
    const items = buildZenTimeline([
      workResultEvent("session.failed", "1aa90ab5-cf46-4643-9985-f6fd26c9526b"),
    ]);
    const enriched = attachBrainWorkEventActions(
      items,
      () => {},
      new Set([
        "brain-agent-zen-telegram-performance-publish-1786011456826849565:@7730",
      ]),
    );
    assertBrainWorkEventCard(
      enriched,
      "session.failed",
      "1aa90ab5-cf46-4643-9985-f6fd26c9526b",
    );
    if (enriched[0]?.type !== "brain-work-event" || !enriched[0].onPress) {
      throw new Error("expected actionable work card");
    }
  });

  test("InterfaceTimelineItemView owns BrainWorkEventCard for work events", () => {
    const source = readFileSync(
      join(import.meta.dir, "../terminal/InterfaceTimelineItemView.tsx"),
      "utf8",
    );
    expect(source).toContain('item.type === "brain-work-event"');
    expect(source).toContain("<BrainWorkEventCard item={item} chrome={chrome} />");
    expect(source).not.toContain("zen_work_event");
  });

  test("raw zen_work_event user envelopes do not project as message cards", () => {
    const envelope = [
      "<zen_work_event>",
      JSON.stringify({
        event_id: "1aa90ab5-cf46-4643-9985-f6fd26c9526b",
        work_id: "ae621005-929b-49b5-9d42-fa476d42d3f3",
        work_title: "zen-telegram-performance-publish",
        kind: "session.failed",
        source:
          "zen-telegram-performance-publish (brain-agent-zen-telegram-performance-publish-1786011456826849565:@7730)",
        summary: "Delegated provider process or pane is no longer live",
        next_action: "Inspect the delegated Session failure.",
        context_ref: "worklog/2026-08-06-zen-telegram-performance-publish.md",
        payload_ref:
          "session:brain-agent-zen-telegram-performance-publish-1786011456826849565:@7730",
      }),
      "</zen_work_event>",
    ].join("\n");
    // If a transport envelope still reaches the App, it must not be mistaken
    // for a work_result card. Daemon sanitization owns omission; this proves
    // the App does not invent a brain-work-event from raw user text.
    const items = buildZenTimeline([
      {
        id: "provider-envelope-1",
        seq: 1,
        timestamp: "2026-08-06T10:19:43.328Z",
        kind: "user_message",
        role: "user",
        body: envelope,
      },
    ]);
    expect(items.map((item) => item.type)).toEqual(["message"]);
    expect(items[0]?.type === "message" && items[0].body).toContain(
      "zen_work_event",
    );
    expect(items.some((item) => item.type === "brain-work-event")).toBe(false);
  });
});
