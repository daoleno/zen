import { useMemo } from "react";
import type { CodexConversationEvent } from "../../services/codexConversation";
import type { ChatCommandEvent } from "./CodexChatSession";
import {
  buildZenTimeline,
  mergeChatCommandEventsIntoTimeline,
} from "./CodexTimelineModel";

export function useCodexTimelineItems({
  events,
  chatCommandEvents,
}: {
  events: CodexConversationEvent[];
  chatCommandEvents: ChatCommandEvent[];
}) {
  return useMemo(
    () =>
      mergeChatCommandEventsIntoTimeline(
        buildZenTimeline(events),
        chatCommandEvents,
      ),
    [chatCommandEvents, events],
  );
}
