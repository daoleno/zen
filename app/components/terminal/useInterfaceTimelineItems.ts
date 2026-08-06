import { useMemo, useRef } from "react";
import type {
  CodexConversationEvent,
  ProviderActivity,
} from "../../services/codexConversation";
import type { PendingUserMessage } from "./InterfaceChatSession";
import {
  buildZenTimeline,
  attachBrainWorkEventActions,
  mergeRunningActivityIntoTimeline,
  mergePendingUserMessagesIntoTimeline,
} from "./InterfaceTimelineModel";
import type { ZenTimelineItem } from "./InterfaceTimelineItemView";
import type { BrainWorkResultEvent } from "../brain/brainWorkEvent";

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

  const providerTimelineItems = useMemo(
    () =>
      attachBrainWorkEventActions(
        buildZenTimeline(events),
        onBrainWorkEventActivate,
        openSessionIds,
      ),
    [events, onBrainWorkEventActivate, openSessionIds],
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
        previousEntry && timelineItemsEqual(previousEntry.item, item)
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

function timelineItemsEqual(left: ZenTimelineItem, right: ZenTimelineItem) {
  if (left === right) {
    return true;
  }
  if (
    left.type !== right.type ||
    left.id !== right.id ||
    left.timestamp !== right.timestamp
  ) {
    return false;
  }
  if (left.type === "message" && right.type === "message") {
    return (
      left.role === right.role &&
      left.body === right.body &&
      left.pending === right.pending &&
      left.pendingLifecycle === right.pendingLifecycle &&
      left.pendingLifecycleLabel === right.pendingLifecycleLabel &&
      left.pendingFailureMessage === right.pendingFailureMessage &&
      left.onRetryPending === right.onRetryPending &&
      left.streaming === right.streaming &&
      left.turnFocusAnchorId === right.turnFocusAnchorId &&
      attachmentsEqual(left.attachments, right.attachments) &&
      left.heartbeatWake === right.heartbeatWake
    );
  }
  if (left.type === "plan" && right.type === "plan") {
    return (
      left.explanation === right.explanation &&
      left.steps.length === right.steps.length &&
      left.steps.every(
        (step, index) =>
          step.step === right.steps[index]?.step &&
          step.status === right.steps[index]?.status,
      )
    );
  }
  if (left.type === "activity" && right.type === "activity") {
    return (
      left.statusKey === right.statusKey &&
      left.title === right.title &&
      left.tone === right.tone &&
      left.icon === right.icon &&
      left.activityKind === right.activityKind &&
      left.streaming === right.streaming &&
      left.detail === right.detail &&
      left.body === right.body &&
      left.bodyKind === right.bodyKind &&
      left.commandText === right.commandText &&
      left.queryText === right.queryText &&
      left.statusLine === right.statusLine &&
      left.previewPath === right.previewPath &&
      left.defaultExpanded === right.defaultExpanded &&
      left.accessibilityLabel === right.accessibilityLabel &&
      left.providerToolId === right.providerToolId &&
      stringArraysEqual(left.files, right.files) &&
      patchSummariesEqual(left.fileSummaries, right.fileSummaries) &&
      JSON.stringify(left.developerDetails) ===
        JSON.stringify(right.developerDetails) &&
      JSON.stringify(left.children) === JSON.stringify(right.children)
    );
  }
  if (
    left.type === "brain-work-event" &&
    right.type === "brain-work-event"
  ) {
    return (
      JSON.stringify(left.event) === JSON.stringify(right.event) &&
      left.onPress === right.onPress
    );
  }
  return false;
}

function attachmentsEqual(
  left: Extract<ZenTimelineItem, { type: "message" }>["attachments"],
  right: Extract<ZenTimelineItem, { type: "message" }>["attachments"],
) {
  if (left === right) {
    return true;
  }
  if (left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (
      left[index]?.name !== right[index]?.name ||
      left[index]?.path !== right[index]?.path
    ) {
      return false;
    }
  }
  return true;
}

function stringArraysEqual(left?: string[], right?: string[]) {
  if (left === right) {
    return true;
  }
  if (!left || !right || left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) {
      return false;
    }
  }
  return true;
}

function patchSummariesEqual(
  left?: Extract<ZenTimelineItem, { type: "activity" }>["fileSummaries"],
  right?: Extract<ZenTimelineItem, { type: "activity" }>["fileSummaries"],
) {
  if (left === right) {
    return true;
  }
  if (!left || !right || left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    const leftFile = left[index];
    const rightFile = right[index];
    if (
      leftFile?.path !== rightFile?.path ||
      leftFile?.movePath !== rightFile?.movePath ||
      leftFile?.operation !== rightFile?.operation ||
      leftFile?.added !== rightFile?.added ||
      leftFile?.removed !== rightFile?.removed
    ) {
      return false;
    }
  }
  return true;
}
