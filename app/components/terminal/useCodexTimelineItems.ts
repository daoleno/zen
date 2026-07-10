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
  }, [active, events, pendingSlashCommands, pendingUserMessages]);
}

function timelineItemsEqual(left: ZenTimelineItem, right: ZenTimelineItem) {
  if (left === right) {
    return true;
  }
  if (left.type !== right.type || left.id !== right.id || left.timestamp !== right.timestamp) {
    return false;
  }
  if (left.type === "message" && right.type === "message") {
    return (
      left.role === right.role &&
      left.body === right.body &&
      left.pending === right.pending &&
      left.pendingLifecycle === right.pendingLifecycle &&
      left.pendingLifecycleLabel === right.pendingLifecycleLabel &&
      attachmentsEqual(left.attachments, right.attachments) &&
      left.heartbeatWake === right.heartbeatWake
    );
  }
  if (left.type === "plan" && right.type === "plan") {
    return (
      left.explanation === right.explanation &&
      left.steps.length === right.steps.length &&
      left.steps.every((step, index) =>
        step.step === right.steps[index]?.step &&
        step.status === right.steps[index]?.status
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
      left.detail === right.detail &&
      left.body === right.body &&
      left.bodyKind === right.bodyKind &&
      left.previewPath === right.previewPath &&
      left.defaultExpanded === right.defaultExpanded &&
      stringArraysEqual(left.files, right.files) &&
      patchSummariesEqual(left.fileSummaries, right.fileSummaries)
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
