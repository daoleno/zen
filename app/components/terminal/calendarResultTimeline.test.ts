import { describe, expect, test } from "bun:test";
import {
  isProviderActivityRunning,
  type CodexConversationEvent,
} from "../../services/codexConversation";
import { buildZenTimeline } from "./InterfaceTimelineModel";

describe("Calendar result timeline", () => {
  const failedResult: CodexConversationEvent = {
    id: "calendar_result:item:run",
    seq: 99,
    timestamp: "2026-07-14T01:01:00Z",
    kind: "status",
    title: "Daily Hacker News failed",
    body: "Linked Work is no longer observable.",
    status: "failed",
    source: "calendar_result",
  };

  test("renders a terminal failure as a compact chronological Calendar activity", () => {
    const laterAssistant: CodexConversationEvent = {
      id: "assistant-later",
      seq: 2,
      timestamp: "2026-07-14T01:02:00Z",
      kind: "assistant_message",
      role: "assistant",
      body: "Later answer",
    };

    const timeline = buildZenTimeline([laterAssistant, failedResult]);
    expect(timeline.map((item) => item.id)).toEqual([
      "calendar_result:item:run",
      "assistant-later",
    ]);
    expect(timeline[0]).toMatchObject({
      type: "activity",
      title: "Daily Hacker News failed",
      tone: "failed",
      icon: "calendar-outline",
      detail: "Linked Work is no longer observable.",
      bodyKind: undefined,
    });
  });

  test("a terminal Calendar result cannot keep the composer in Working state", () => {
    expect(isProviderActivityRunning(undefined)).toBe(false);
  });
});
