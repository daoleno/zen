import type { CodexConversationEvent } from "../../services/codexConversation";

const BASE_MS = Date.parse("2026-08-06T00:00:00.000Z");

/**
 * Deterministic mixed conversation fixtures for projection benchmarks.
 * Keeps near 1:1 event→row mapping so settled-row identity budgets stay meaningful
 * (messages + sparse non-groupable tool/work cards; no exploration command runs).
 */
export function makeMixedTimelineEvents(count: number): CodexConversationEvent[] {
  const events: CodexConversationEvent[] = [];
  for (let index = 0; index < count; index += 1) {
    const timestamp = new Date(BASE_MS + index * 60_000).toISOString();
    if (index % 25 === 24) {
      events.push({
        id: `tool-${index}`,
        seq: index,
        kind: "tool",
        tool_name: "Grep",
        input: `{"pattern":"fixture","path":"app"}`,
        body: `match-${index}`,
        status: "done",
        timestamp,
      });
      continue;
    }
    if (index % 17 === 16) {
      events.push({
        id: `work-${index}`,
        seq: index,
        kind: "status",
        source: "work_result",
        status: "session.done",
        work_id: `work-${index}`,
        title: `Work ${index}`,
        body: `Summary ${index}`,
        timestamp,
        work_review_state: "resolved",
        work_session_state: "not_required",
        work_result_current: true,
      });
      continue;
    }
    if (index % 2 === 0) {
      events.push({
        id: `user-${index}`,
        seq: index,
        kind: "user_message",
        role: "user",
        body: `User message ${index}`,
        timestamp,
      });
      continue;
    }
    events.push({
      id: `assistant-${index}`,
      seq: index,
      kind: "assistant_message",
      role: "assistant",
      body: `Assistant reply ${index}`,
      timestamp,
      partial: false,
    });
  }
  return events;
}

export function firstAssistantEventId(events: CodexConversationEvent[]) {
  const found = events.find((event) => event.kind === "assistant_message");
  if (!found) {
    throw new Error("fixture requires an assistant_message");
  }
  return found.id;
}

export function withAssistantBodyRevision(
  events: CodexConversationEvent[],
  assistantId: string,
  revision: number,
): CodexConversationEvent[] {
  const body = `Assistant streaming body revision ${revision} `.repeat(
    4 + (revision % 7),
  );
  return events.map((event) =>
    event.id === assistantId
      ? {
          ...event,
          body,
          partial: true,
          status: "streaming",
        }
      : event,
  );
}
