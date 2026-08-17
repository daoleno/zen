import {
  dayKeyFromTimestamp,
  formatDateDividerLabel,
} from "../../constants/telegramPresentation";
import type { ZenTimelineItem } from "./InterfaceTimelineItemView";
import type { ZenDateDividerItem } from "./InterfaceTimelineDateDivider";
import {
  isTimelineProjectionPerfEnabled,
  recordTimelineRenderProjectionSample,
} from "./timelineProjectionPerf";

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

export type TimelineRenderProjectionMode =
  | "full"
  | "stable"
  | "update"
  | "append"
  | "prepend";

export type TimelineRenderProjectionCache = {
  sourceItems: ZenTimelineItem[];
  renderItems: TimelineRenderItem[];
  showDateDividers: boolean;
};

export type TimelineRenderProjectionResult = {
  items: TimelineRenderItem[];
  cache: TimelineRenderProjectionCache;
  mode: TimelineRenderProjectionMode;
  changedSourceCount: number;
  stableRenderReuse: number;
  stableRenderChurn: number;
};

/**
 * Projects canonical oldest-first timeline items into inverted-list data.
 * Same-topology row updates replace only the affected render rows; proven
 * newest appends and oldest prepends rebuild one boundary day and retain the
 * rest of the mounted row references. Structural ambiguity falls back to the
 * canonical full builder rather than risking an incorrect divider or group.
 */
export function projectTimelineRenderItems(
  sourceItems: ZenTimelineItem[],
  options: { showDateDividers: boolean },
  previous?: TimelineRenderProjectionCache | null,
): TimelineRenderProjectionResult {
  const measure = isTimelineProjectionPerfEnabled();
  const started = measure ? timelineRenderNowMs() : 0;
  const result = projectTimelineRenderItemsUninstrumented(
    sourceItems,
    options,
    previous,
  );
  if (measure) {
    recordTimelineRenderProjectionSample({
      mode: result.mode,
      durationMs: timelineRenderNowMs() - started,
      sourceItemCount: sourceItems.length,
      renderItemCount: result.items.length,
      changedSourceCount: result.changedSourceCount,
      stableRenderReuse: result.stableRenderReuse,
      stableRenderChurn: result.stableRenderChurn,
    });
  }
  return result;
}

function projectTimelineRenderItemsUninstrumented(
  sourceItems: ZenTimelineItem[],
  options: { showDateDividers: boolean },
  previous?: TimelineRenderProjectionCache | null,
): TimelineRenderProjectionResult {
  if (!previous || previous.showDateDividers !== options.showDateDividers) {
    return fullTimelineRenderProjection(sourceItems, options.showDateDividers);
  }
  if (sourceItems === previous.sourceItems) {
    return timelineRenderProjectionResult(
      sourceItems,
      previous.renderItems,
      options.showDateDividers,
      "stable",
      0,
      previous.renderItems.length,
      0,
    );
  }
  if (sourceItems.length === previous.sourceItems.length) {
    return projectStableTopologyUpdates(
      sourceItems,
      previous,
      options.showDateDividers,
    );
  }
  if (sourceItems.length > previous.sourceItems.length) {
    const append = projectProvenAppend(
      sourceItems,
      previous,
      options.showDateDividers,
    );
    if (append) {
      return append;
    }
    const prepend = projectProvenPrepend(
      sourceItems,
      previous,
      options.showDateDividers,
    );
    if (prepend) {
      return prepend;
    }
  }
  return fullTimelineRenderProjection(sourceItems, options.showDateDividers);
}

function timelineRenderNowMs() {
  const perf = (globalThis as { performance?: { now(): number } }).performance;
  return perf?.now?.() ?? Date.now();
}

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
    if (dayKey && dayKey !== previousDayKey) {
      previousDayKey = dayKey;
      previousDayLabel = formatDateDividerLabel(item.timestamp);
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

function fullTimelineRenderProjection(
  sourceItems: ZenTimelineItem[],
  showDateDividers: boolean,
): TimelineRenderProjectionResult {
  const renderItems = buildTimelineRenderItems(sourceItems.slice().reverse(), {
    showDateDividers,
  });
  return timelineRenderProjectionResult(
    sourceItems,
    renderItems,
    showDateDividers,
    "full",
    sourceItems.length,
    0,
    renderItems.length,
  );
}

function projectStableTopologyUpdates(
  sourceItems: ZenTimelineItem[],
  previous: TimelineRenderProjectionCache,
  showDateDividers: boolean,
): TimelineRenderProjectionResult {
  const changedSourceIndices: number[] = [];
  for (let index = 0; index < sourceItems.length; index += 1) {
    const prior = previous.sourceItems[index];
    const next = sourceItems[index];
    if (prior === next) {
      continue;
    }
    if (!timelineItemTopologyEqual(prior, next)) {
      return fullTimelineRenderProjection(sourceItems, showDateDividers);
    }
    changedSourceIndices.push(index);
  }
  if (changedSourceIndices.length === 0) {
    return timelineRenderProjectionResult(
      sourceItems,
      previous.renderItems,
      showDateDividers,
      "stable",
      0,
      previous.renderItems.length,
      0,
    );
  }

  const renderItems = previous.renderItems.slice();
  const renderIndexById =
    changedSourceIndices.length > 1
      ? new Map(renderItems.map((item, index) => [item.id, index] as const))
      : null;
  let stableRenderChurn = 0;
  for (const sourceIndex of changedSourceIndices) {
    const itemId = sourceItems[sourceIndex]!.id;
    const renderIndex =
      renderIndexById?.get(itemId) ??
      findRenderItemIndexById(renderItems, itemId);
    if (renderIndex < 0) {
      return fullTimelineRenderProjection(sourceItems, showDateDividers);
    }
    const priorRenderItem = renderItems[renderIndex]!;
    const nextSourceItem = sourceItems[sourceIndex]!;
    renderItems[renderIndex] =
      priorRenderItem.type === "message" && nextSourceItem.type === "message"
        ? {
            ...nextSourceItem,
            presentation: priorRenderItem.presentation,
            sourceItem: nextSourceItem,
          }
        : nextSourceItem;
    stableRenderChurn += 1;
  }
  return timelineRenderProjectionResult(
    sourceItems,
    renderItems,
    showDateDividers,
    "update",
    changedSourceIndices.length,
    renderItems.length - stableRenderChurn,
    stableRenderChurn,
  );
}

function projectProvenAppend(
  sourceItems: ZenTimelineItem[],
  previous: TimelineRenderProjectionCache,
  showDateDividers: boolean,
): TimelineRenderProjectionResult | null {
  const previousCount = previous.sourceItems.length;
  const addedCount = sourceItems.length - previousCount;
  if (previousCount === 0 || addedCount <= 0) {
    return null;
  }
  for (let index = 0; index < previousCount; index += 1) {
    if (sourceItems[index] !== previous.sourceItems[index]) {
      return null;
    }
  }
  const boundaryStart = startOfDayGroup(
    previous.sourceItems,
    previousCount - 1,
  );
  if (boundaryStart < 0) {
    return null;
  }
  const oldBoundary = buildTimelineRenderItems(
    previous.sourceItems.slice(boundaryStart).reverse(),
    { showDateDividers },
  );
  const nextBoundary = stabilizeTimelineRenderItems(
    oldBoundary,
    buildTimelineRenderItems(sourceItems.slice(boundaryStart).reverse(), {
      showDateDividers,
    }),
  );
  const untouched = previous.renderItems.slice(oldBoundary.length);
  const renderItems = nextBoundary.concat(untouched);
  const stableReuse =
    untouched.length + countBoundaryReferenceReuse(oldBoundary, nextBoundary);
  const churn = renderItems.length - stableReuse;
  return timelineRenderProjectionResult(
    sourceItems,
    renderItems,
    showDateDividers,
    "append",
    addedCount,
    stableReuse,
    churn,
  );
}

function projectProvenPrepend(
  sourceItems: ZenTimelineItem[],
  previous: TimelineRenderProjectionCache,
  showDateDividers: boolean,
): TimelineRenderProjectionResult | null {
  const previousCount = previous.sourceItems.length;
  const addedCount = sourceItems.length - previousCount;
  if (previousCount === 0 || addedCount <= 0) {
    return null;
  }
  for (let index = 0; index < previousCount; index += 1) {
    if (sourceItems[addedCount + index] !== previous.sourceItems[index]) {
      return null;
    }
  }
  const boundaryEnd = endOfDayGroup(previous.sourceItems, 0);
  if (boundaryEnd <= 0) {
    return null;
  }
  const oldBoundary = buildTimelineRenderItems(
    previous.sourceItems.slice(0, boundaryEnd).reverse(),
    { showDateDividers },
  );
  const nextBoundary = stabilizeTimelineRenderItems(
    oldBoundary,
    buildTimelineRenderItems(
      sourceItems.slice(0, addedCount + boundaryEnd).reverse(),
      { showDateDividers },
    ),
  );
  const untouchedCount = previous.renderItems.length - oldBoundary.length;
  const untouched = previous.renderItems.slice(0, untouchedCount);
  const renderItems = untouched.concat(nextBoundary);
  const stableReuse =
    untouched.length + countBoundaryReferenceReuse(oldBoundary, nextBoundary);
  const churn = renderItems.length - stableReuse;
  return timelineRenderProjectionResult(
    sourceItems,
    renderItems,
    showDateDividers,
    "prepend",
    addedCount,
    stableReuse,
    churn,
  );
}

function timelineRenderProjectionResult(
  sourceItems: ZenTimelineItem[],
  renderItems: TimelineRenderItem[],
  showDateDividers: boolean,
  mode: TimelineRenderProjectionMode,
  changedSourceCount: number,
  stableRenderReuse: number,
  stableRenderChurn: number,
): TimelineRenderProjectionResult {
  return {
    items: renderItems,
    cache: { sourceItems, renderItems, showDateDividers },
    mode,
    changedSourceCount,
    stableRenderReuse,
    stableRenderChurn,
  };
}

function timelineItemTopologyEqual(
  previous: ZenTimelineItem | undefined,
  next: ZenTimelineItem | undefined,
) {
  if (
    !previous ||
    !next ||
    previous.id !== next.id ||
    previous.type !== next.type ||
    previous.timestamp !== next.timestamp
  ) {
    return false;
  }
  return (
    previous.type !== "message" ||
    (next.type === "message" && previous.role === next.role)
  );
}

function findRenderItemIndexById(
  renderItems: TimelineRenderItem[],
  id: string,
) {
  for (let index = 0; index < renderItems.length; index += 1) {
    if (renderItems[index]!.id === id) {
      return index;
    }
  }
  return -1;
}

function startOfDayGroup(items: ZenTimelineItem[], index: number) {
  const dayKey = dayKeyFromTimestamp(items[index]?.timestamp);
  if (!dayKey) {
    return -1;
  }
  let start = index;
  while (
    start > 0 &&
    dayKeyFromTimestamp(items[start - 1]?.timestamp) === dayKey
  ) {
    start -= 1;
  }
  return start;
}

function endOfDayGroup(items: ZenTimelineItem[], index: number) {
  const dayKey = dayKeyFromTimestamp(items[index]?.timestamp);
  if (!dayKey) {
    return -1;
  }
  let end = index + 1;
  while (
    end < items.length &&
    dayKeyFromTimestamp(items[end]?.timestamp) === dayKey
  ) {
    end += 1;
  }
  return end;
}

function countBoundaryReferenceReuse(
  previous: TimelineRenderItem[],
  next: TimelineRenderItem[],
) {
  const previousReferences = new Set(previous);
  let reuse = 0;
  for (const item of next) {
    if (previousReferences.has(item)) {
      reuse += 1;
    }
  }
  return reuse;
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
  if (previous.type === "message" && next.type === "message") {
    return (
      previous.sourceItem === next.sourceItem &&
      messagePresentationsEqual(previous.presentation, next.presentation)
    );
  }
  // Activity / plan / work-event rows are the timeline items themselves — they
  // never receive a grouping wrapper. Referential equality is handled above;
  // a distinct object with the same id means projection allocated a new row.
  return false;
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
