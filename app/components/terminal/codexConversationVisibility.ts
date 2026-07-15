import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";

export function shouldDropProviderChatNoiseEvent(
  source: CodexConversation["source"],
  kind: CodexConversationEvent["kind"],
) {
  return source === "grok_session" && kind === "plan";
}

export function shouldDropStructuredChatEvent(
  source: CodexConversation["source"],
  event: CodexConversationEvent,
) {
  return (
    event.source === "terminal_snapshot" ||
    shouldDropProviderChatNoiseEvent(source, event.kind)
  );
}
