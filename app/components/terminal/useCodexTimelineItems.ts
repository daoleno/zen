import { useMemo } from "react";
import type { CodexConversationEvent } from "../../services/codexConversation";
import type {
  PendingAssistantMessage,
  PendingUserMessage,
} from "./CodexChatSession";
import {
  buildZenTimeline,
  mergePendingAssistantMessagesIntoTimeline,
  mergePendingUserMessagesIntoTimeline,
} from "./CodexTimelineModel";

export function useCodexTimelineItems({
  events,
  pendingUserMessages,
  pendingAssistantMessages,
}: {
  events: CodexConversationEvent[];
  pendingUserMessages: PendingUserMessage[];
  pendingAssistantMessages: PendingAssistantMessage[];
}) {
  return useMemo(
    () =>
      mergePendingAssistantMessagesIntoTimeline(
        mergePendingUserMessagesIntoTimeline(
          buildZenTimeline(events),
          pendingUserMessages,
        ),
        pendingAssistantMessages,
      ),
    [events, pendingAssistantMessages, pendingUserMessages],
  );
}
