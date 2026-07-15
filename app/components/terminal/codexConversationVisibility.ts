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
