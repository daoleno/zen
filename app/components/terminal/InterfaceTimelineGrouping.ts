import {
  dayKeyFromTimestamp,
  formatDateDividerLabel,
} from "../../constants/telegramPresentation";
import type { ZenTimelineItem } from "./InterfaceTimelineItemView";
import type { ZenDateDividerItem } from "./InterfaceTimelineDateDivider";

export type MessageGroupPosition = "single" | "first" | "middle" | "last";

export type MessagePresentation = {
  showAvatar: boolean;
  groupPosition: MessageGroupPosition;
  compactTop: boolean;
  compactBottom: boolean;
};

export type GroupedTimelineItem = ZenTimelineItem & {
  presentation?: MessagePresentation;
  sourceItem?: ZenTimelineItem;
};

export type TimelineRenderItem = GroupedTimelineItem | ZenDateDividerItem;

export function buildTimelineRenderItems(
  items: ZenTimelineItem[],
  options?: { showDateDividers?: boolean },
): TimelineRenderItem[] {
  const withDividers =
    options?.showDateDividers === false ? items : injectDateDividers(items);
  return dedupeTimelineRenderItems(annotateMessageGrouping(withDividers));
}

export function stabilizeTimelineRenderItems(
  previous: TimelineRenderItem[],
  next: TimelineRenderItem[],
): TimelineRenderItem[] {
  if (previous.length === 0) {
    return next;
  }
  const previousById = new Map(previous.map((item) => [item.id, item]));
  let changed = previous.length !== next.length;
  const stable = next.map((item, index) => {
    const prior = previousById.get(item.id);
    const resolved =
      prior && timelineRenderItemsEquivalent(prior, item) ? prior : item;
    if (previous[index] !== resolved) {
      changed = true;
    }
    return resolved;
  });
  return changed ? stable : previous;
}

function dedupeTimelineRenderItems(
  items: TimelineRenderItem[],
): TimelineRenderItem[] {
  const seen = new Set<string>();
  const output: TimelineRenderItem[] = [];
  for (const item of items) {
    if (seen.has(item.id)) {
      continue;
    }
    seen.add(item.id);
    output.push(item);
  }
  return output;
}

/**
 * Insert date dividers for an inverted (newest-first) timeline.
 * In an inverted FlatList, earlier array indices sit nearer the composer,
 * so a day's divider must come *after* that day's items in the array —
 * visually above them, matching Telegram.
 */
function injectDateDividers(items: ZenTimelineItem[]): TimelineRenderItem[] {
  const output: TimelineRenderItem[] = [];
  let previousDayKey: string | null = null;
  let previousDayLabel: string | null = null;

  for (const item of items) {
    const dayKey = dayKeyFromTimestamp(item.timestamp);
    const dividerLabel = formatDateDividerLabel(item.timestamp);
    if (
      previousDayKey &&
      previousDayLabel &&
      dayKey &&
      dayKey !== previousDayKey
    ) {
      output.push({
        type: "date-divider",
        id: `date:${previousDayKey}`,
        label: previousDayLabel,
      });
    }
    output.push(item);
    if (dayKey && dividerLabel) {
      previousDayKey = dayKey;
      previousDayLabel = dividerLabel;
    }
  }

  // Oldest day still needs a header at the visual top of the thread.
  if (previousDayKey && previousDayLabel) {
    output.push({
      type: "date-divider",
      id: `date:${previousDayKey}`,
      label: previousDayLabel,
    });
  }

  return output;
}

function annotateMessageGrouping(
  items: TimelineRenderItem[],
): TimelineRenderItem[] {
  return items.map((item, index) => {
    if (item.type !== "message") {
      return item;
    }

    const newer = itemAt(items, index - 1);
    const older = itemAt(items, index + 1);
    const sameRoleNewer = isSameMessageGroup(newer, item);
    const sameRoleOlder = isSameMessageGroup(older, item);

    const groupPosition: MessageGroupPosition =
      !sameRoleOlder && !sameRoleNewer
        ? "single"
        : !sameRoleOlder && sameRoleNewer
          ? "last"
          : sameRoleOlder && !sameRoleNewer
            ? "first"
            : "middle";

    const presentation: MessagePresentation = {
      showAvatar: false,
      groupPosition,
      compactTop: sameRoleOlder,
      compactBottom: sameRoleNewer,
    };

    return {
      ...item,
      presentation,
      sourceItem: item,
    };
  });
}

function timelineRenderItemsEquivalent(
  previous: TimelineRenderItem,
  next: TimelineRenderItem,
) {
  if (previous === next) {
    return true;
  }
  if (previous.type !== next.type || previous.id !== next.id) {
    return false;
  }
  if (previous.type === "date-divider" && next.type === "date-divider") {
    return previous.label === next.label;
  }
  if (previous.type !== "message" || next.type !== "message") {
    return false;
  }
  return (
    previous.sourceItem === next.sourceItem &&
    messagePresentationsEqual(previous.presentation, next.presentation)
  );
}

function messagePresentationsEqual(
  previous: MessagePresentation | undefined,
  next: MessagePresentation | undefined,
) {
  return (
    previous === next ||
    (previous?.showAvatar === next?.showAvatar &&
      previous?.groupPosition === next?.groupPosition &&
      previous?.compactTop === next?.compactTop &&
      previous?.compactBottom === next?.compactBottom)
  );
}

function itemAt(
  items: TimelineRenderItem[],
  index: number,
): GroupedTimelineItem | null {
  const candidate = items[index];
  return candidate?.type === "message" ? candidate : null;
}

function isSameMessageGroup(
  left: GroupedTimelineItem | null,
  right: GroupedTimelineItem,
): boolean {
  if (!left || left.type !== "message" || right.type !== "message") {
    return false;
  }
  return left.role === right.role;
}
