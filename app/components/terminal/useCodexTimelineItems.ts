import { useMemo, useRef } from "react";
import type { CodexConversationEvent } from "../../services/codexConversation";
import type {
  PendingSlashCommand,
  PendingUserMessage,
} from "./CodexChatSession";
import {
  buildZenTimeline,
  mergeActiveTurnIntoTimeline,
  mergePendingSlashCommandsIntoTimeline,
  mergePendingUserMessagesIntoTimeline,
} from "./CodexTimelineModel";
import type { ZenTimelineItem } from "./CodexTimelineItemView";

type StableTimelineEntry = {
  item: ZenTimelineItem;
  fingerprint: string;
};

export function useCodexTimelineItems({
  events,
  pendingUserMessages,
  pendingSlashCommands,
  active,
}: {
  events: CodexConversationEvent[];
  pendingUserMessages: PendingUserMessage[];
  pendingSlashCommands: PendingSlashCommand[];
  active?: boolean;
}) {
  const previousRef = useRef<{
    byId: Map<string, StableTimelineEntry>;
    items: ZenTimelineItem[];
  }>({
    byId: new Map(),
    items: [],
  });

  return useMemo(() => {
    const nextItems = mergePendingSlashCommandsIntoTimeline(
      mergeActiveTurnIntoTimeline(
        mergePendingUserMessagesIntoTimeline(
          buildZenTimeline(events),
          pendingUserMessages,
        ),
        active,
      ),
      pendingSlashCommands,
    );
    const previous = previousRef.current;
    const nextById = new Map<string, StableTimelineEntry>();
    let changed =
      previous.items.length !== nextItems.length;
    const stableItems = nextItems.map((item, index) => {
      const fingerprint = timelineItemFingerprint(item);
      const previousEntry = previous.byId.get(item.id);
      const stableItem =
        previousEntry?.fingerprint === fingerprint
          ? previousEntry.item
          : item;
      if (previous.items[index] !== stableItem) {
        changed = true;
      }
      nextById.set(item.id, {
        item: stableItem,
        fingerprint,
      });
      return stableItem;
    });
    if (!changed) {
      return previous.items;
    }
    previousRef.current = {
      byId: nextById,
      items: stableItems,
    };
    return stableItems;
  }, [active, events, pendingSlashCommands, pendingUserMessages]);
}

function timelineItemFingerprint(item: ZenTimelineItem) {
  return JSON.stringify(item);
}
