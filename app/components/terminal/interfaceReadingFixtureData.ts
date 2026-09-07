import type { CodexConversationEvent } from "../../services/codexConversation";

export function readingFixtureMessage(
  index: number,
  body?: string,
): CodexConversationEvent {
  return {
    id: `reading-${index}`,
    seq: index,
    kind: "assistant_message",
    role: "assistant",
    body:
      body ??
      `## Message ${index}\n\nReading anchor ${index}.\n\n第一行保留位置。第二行用于验证历史内容。\n\n${"Stable message content. ".repeat((Math.abs(index) % 4) + 1)}`,
    timestamp: new Date(1788825600000 + index * 1000).toISOString(),
  };
}
