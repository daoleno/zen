import { describe, expect, test } from "bun:test";

import { normalizeCodexConversation } from "./codexConversation";

describe("Codex conversation Goal context projection", () => {
  test("drops typed internal Goal events", () => {
    const conversation = normalizeCodexConversation({
      available: true,
      events: [
        {
          id: "goal-context",
          seq: 1,
          kind: "status",
          source: "goal",
          title: "codex_internal_context",
          body: "hidden objective",
        },
        {
          id: "answer",
          seq: 2,
          kind: "assistant_message",
          body: "Visible answer",
        },
      ],
    });

    expect(conversation.events).toEqual([
      expect.objectContaining({ id: "answer", body: "Visible answer" }),
    ]);
  });

  test("removes embedded typed Goal XML from every display field", () => {
    const hidden =
      '<codex_internal_context source="goal">hidden objective</codex_internal_context>';
    const conversation = normalizeCodexConversation({
      available: true,
      events: [
        {
          id: "answer",
          seq: 1,
          kind: "assistant_message",
          title: `Title ${hidden}`,
          body: `Before\n${hidden}\nAfter`,
          command: `run ${hidden}`,
          input: `input ${hidden}`,
          output: `output ${hidden}`,
          explanation: `explanation ${hidden}`,
          plan: [{ step: `step ${hidden}`, status: "pending" }],
        },
        {
          id: "only-internal",
          seq: 2,
          kind: "user_message",
          body: hidden,
        },
      ],
    });

    expect(conversation.events).toHaveLength(1);
    expect(conversation.events[0]).toMatchObject({
      id: "answer",
      title: "Title",
      body: "Before\n\nAfter",
      command: "run",
      input: "input",
      output: "output",
      explanation: "explanation",
      plan: [{ step: "step", status: "pending" }],
    });
    expect(JSON.stringify(conversation)).not.toContain(
      "codex_internal_context",
    );
    expect(JSON.stringify(conversation)).not.toContain("hidden objective");
  });
});
