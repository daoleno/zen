import { useMemo } from "react";
import type { CodexConversationEvent } from "../../services/codexConversation";
import type {
  ChatCommandEvent,
  PendingUserMessage,
} from "./CodexChatSession";
import {
  buildZenTimeline,
  mergeChatCommandEventsIntoTimeline,
  mergePendingUserMessagesIntoTimeline,
} from "./CodexTimelineModel";

export function useCodexTimelineItems({
  events,
  chatCommandEvents,
  pendingUserMessages,
}: {
  events: CodexConversationEvent[];
  chatCommandEvents: ChatCommandEvent[];
  pendingUserMessages: PendingUserMessage[];
}) {
  return useMemo(
    () =>
      mergeChatCommandEventsIntoTimeline(
        mergePendingUserMessagesIntoTimeline(
          buildZenTimeline(events),
          pendingUserMessages,
        ),
        chatCommandEvents,
      ),
    [chatCommandEvents, events, pendingUserMessages],
  );
}
