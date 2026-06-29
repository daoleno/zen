import {
  dayKeyFromTimestamp,
  formatDateDividerLabel,
} from "../../constants/telegramPresentation";
import type { ZenTimelineItem } from "./CodexTimelineItemView";
import type { ZenDateDividerItem } from "./CodexTimelineDateDivider";

export type MessageGroupPosition = "single" | "first" | "middle" | "last";

export type MessagePresentation = {
  showAvatar: boolean;
  groupPosition: MessageGroupPosition;
  compactTop: boolean;
  compactBottom: boolean;
};

export type GroupedTimelineItem = ZenTimelineItem & {
  presentation?: MessagePresentation;
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

function injectDateDividers(items: ZenTimelineItem[]): TimelineRenderItem[] {
  const output: TimelineRenderItem[] = [];
  let previousDayKey: string | null = null;

  for (const item of items) {
    const dayKey = dayKeyFromTimestamp(item.timestamp);
    const dividerLabel = formatDateDividerLabel(item.timestamp);
    if (dayKey && dividerLabel && dayKey !== previousDayKey) {
      output.push({
        type: "date-divider",
        id: `date:${dayKey}`,
        label: dividerLabel,
      });
      previousDayKey = dayKey;
    }
    output.push(item);
  }

  return output;
}

function annotateMessageGrouping(items: TimelineRenderItem[]): TimelineRenderItem[] {
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
    };
  });
}

function itemAt(items: TimelineRenderItem[], index: number): GroupedTimelineItem | null {
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