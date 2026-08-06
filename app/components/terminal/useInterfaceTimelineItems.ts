import { useMemo, useRef } from "react";
import type {
  CodexConversationEvent,
  ProviderActivity,
} from "../../services/codexConversation";
import type { PendingUserMessage } from "./InterfaceChatSession";
import {
  attachBrainWorkEventActions,
  mergeRunningActivityIntoTimeline,
  mergePendingUserMessagesIntoTimeline,
} from "./InterfaceTimelineModel";
import type { ZenTimelineItem } from "./InterfaceTimelineItemView";
import type { BrainWorkResultEvent } from "../brain/brainWorkEvent";
import {
  projectZenTimeline,
  type ZenTimelineProjectionCache,
} from "./projectZenTimeline";
import { timelineItemsSemanticEqual } from "./timelineItemsSemanticEqual";

type StableTimelineEntry = {
  item: ZenTimelineItem;
};

export function useInterfaceTimelineItems({
  events,
  pendingUserMessages,
  turnFocusAnchorAliases,
  runningActivity,
  onRetryPendingUserMessage,
  onBrainWorkEventActivate,
  openSessionIds,
}: {
  events: CodexConversationEvent[];
  pendingUserMessages: PendingUserMessage[];
  turnFocusAnchorAliases?: ReadonlyMap<string, string>;
  runningActivity?: ProviderActivity;
  onRetryPendingUserMessage(id: string): void;
  onBrainWorkEventActivate?: (
    event: BrainWorkResultEvent,
    canOpenSession: boolean,
  ) => void;
  openSessionIds?: ReadonlySet<string>;
}) {
  const previousRef = useRef<{
    byId: Map<string, StableTimelineEntry>;
    items: ZenTimelineItem[];
  }>({
    byId: new Map(),
    items: [],
  });
  const projectionCacheRef = useRef<ZenTimelineProjectionCache | null>(null);

  // Pure event→timeline projection. Callback/set identity must not rescan events.
  const projectedTimelineItems = useMemo(() => {
    const projected = projectZenTimeline(
      events,
      projectionCacheRef.current,
    );
    projectionCacheRef.current = projected.cache;
    return projected.items;
  }, [events]);

  const providerTimelineItems = useMemo(
    () =>
      attachBrainWorkEventActions(
        projectedTimelineItems,
        onBrainWorkEventActivate,
        openSessionIds,
      ),
    [projectedTimelineItems, onBrainWorkEventActivate, openSessionIds],
  );
  const providerTimelineItemsWithTurnFocus = useMemo(() => {
    if (!turnFocusAnchorAliases?.size) {
      return providerTimelineItems;
    }
    return providerTimelineItems.map((item) => {
      const turnFocusAnchorId = turnFocusAnchorAliases.get(item.id);
      if (item.type !== "message" || !turnFocusAnchorId) {
        return item;
      }
      return {
        ...item,
        turnFocusAnchorId,
      };
    });
  }, [providerTimelineItems, turnFocusAnchorAliases]);
  const providerTimelineWithActivity = useMemo(
    () =>
      mergeRunningActivityIntoTimeline(
        providerTimelineItemsWithTurnFocus,
        runningActivity,
      ),
    [providerTimelineItemsWithTurnFocus, runningActivity],
  );

  return useMemo(() => {
    const nextItems = mergePendingUserMessagesIntoTimeline(
      providerTimelineWithActivity,
      pendingUserMessages,
      onRetryPendingUserMessage,
    );
    const previous = previousRef.current;
    const nextById = new Map<string, StableTimelineEntry>();
    let changed = previous.items.length !== nextItems.length;
    const stableItems = nextItems.map((item, index) => {
      const previousEntry = previous.byId.get(item.id);
      const stableItem =
        previousEntry &&
        timelineItemsSemanticEqual(previousEntry.item, item)
          ? previousEntry.item
          : item;
      if (previous.items[index] !== stableItem) {
        changed = true;
      }
      nextById.set(item.id, {
        item: stableItem,
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
  }, [
    onRetryPendingUserMessage,
    pendingUserMessages,
    providerTimelineWithActivity,
  ]);
}
