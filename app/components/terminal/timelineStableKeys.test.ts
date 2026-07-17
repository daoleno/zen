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

function tool(
  body: string,
  status: "running" | "done",
): CodexConversationEvent {
  return {
    id: "tool-call-7",
    seq: 7,
    kind: "tool",
    tool_name: "Grep",
    input: `{"pattern":"Tool header","path":"app"}`,
    body,
    status,
    partial: status === "running",
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

  test("same-logical-Tool streaming upserts retain the mounted row key", () => {
    const running = buildZenTimeline([tool("first match", "running")]);
    const done = buildZenTimeline([
      tool("first match\nsecond match", "done"),
    ]);
    const renderedRunning = buildTimelineRenderItems([...running].reverse(), {
      showDateDividers: false,
    });
    const renderedDone = buildTimelineRenderItems([...done].reverse(), {
      showDateDividers: false,
    });

    expect(running[0]?.type).toBe("activity");
    expect(done[0]?.type).toBe("activity");
    expect(renderedRunning[0]?.id).toBe("tool-call-7");
    expect(renderedDone[0]?.id).toBe(renderedRunning[0]?.id);
  });
});
