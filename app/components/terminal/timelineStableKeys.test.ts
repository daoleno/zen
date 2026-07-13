// @ts-nocheck
import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../../services/codexConversation";
import { buildTimelineRenderItems } from "./CodexTimelineGrouping";
import { buildZenTimeline } from "./CodexTimelineModel";

function assistant(body: string): CodexConversationEvent {
  return {
    id: "assistant-message-7",
    seq: 7,
    kind: "assistant_message",
    role: "assistant",
    body,
  };
}

describe("timeline identity", () => {
  test("streaming content changes retain the daemon event key", () => {
    const partial = buildZenTimeline([assistant("partial")]);
    const complete = buildZenTimeline([assistant("complete response")]);

    expect(partial[0]?.id).toBe("assistant-message-7");
    expect(complete[0]?.id).toBe(partial[0]?.id);
  });

  test("render decoration does not replace logical message keys", () => {
    const items = buildZenTimeline([assistant("complete response")]);
    const rendered = buildTimelineRenderItems([...items].reverse(), {
      showDateDividers: false,
    });

    expect(rendered.map((item) => item.id)).toEqual(["assistant-message-7"]);
  });
});
