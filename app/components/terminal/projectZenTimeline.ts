import type { CodexConversationEvent } from "../../services/codexConversation";
import { compareConversationEvents } from "./interfaceConversationReconciliation";
import { buildZenTimelineFromSortedEvents } from "./InterfaceTimelineModel";
import type { ZenTimelineItem } from "./InterfaceTimelineItemView";
import {
  isTimelineProjectionPerfEnabled,
  recordTimelineProjectionSample,
  type TimelineProjectionFallbackReason,
  type TimelineProjectionMode,
} from "./timelineProjectionPerf";

export type ZenTimelineProjectionCache = {
  sortedEvents: CodexConversationEvent[];
  items: ZenTimelineItem[];
  /**
   * Computed on canonical full projection. When false, incremental replacement
   * is unsafe (duplicate event/item ids) and must full-fallback. Inherited on
   * proved same-id incremental updates — never re-scanned on the hot path.
   */
  incrementalSafe: boolean;
  /** Present only when incrementalSafe; O(1) item lookup without findIndex. */
  itemIndexById: ReadonlyMap<string, number> | null;
};

export type ZenTimelineEventOrderSource =
  | "cached-ids"
  | "already-sorted"
  | "sorted";

export type ZenTimelineProjectionResult = {
  items: ZenTimelineItem[];
  cache: ZenTimelineProjectionCache;
  mode: TimelineProjectionMode;
  fallbackReason?: TimelineProjectionFallbackReason;
  dirtyStart?: number;
  dirtyEnd?: number;
  stableRowReuse: number;
  stableRowChurn: number;
  /** How event order was established for this projection (exactly one sort when "sorted"). */
  eventOrder: ZenTimelineEventOrderSource;
};

/**
 * Canonical timeline projection entry.
 * Full path uses `buildZenTimelineFromSortedEvents` after at most one order resolve.
 * Incremental path runs only when a bounded same-id/same-kind message streaming
 * mutation is proven; otherwise falls back without forking presentation semantics.
 *
 * Turn-focus aliases are owned by `useInterfaceTimelineItems` after projection —
 * this API never accepts or caches aliases.
 */
export function projectZenTimeline(
  events: CodexConversationEvent[],
  previous: ZenTimelineProjectionCache | null | undefined,
): ZenTimelineProjectionResult {
  const measure = isTimelineProjectionPerfEnabled();
  const started = measure ? nowMs() : 0;

  if (!previous) {
    return finishFull(
      resolveProjectionEventOrder(events, null),
      started,
      measure,
      "no-cache",
    );
  }

  const ordered = resolveProjectionEventOrder(events, previous);
  const sortedEvents = ordered.sortedEvents;

  if (sortedEvents.length !== previous.sortedEvents.length) {
    const appended = tryProvenSingleEventAppend(
      sortedEvents,
      previous,
      started,
      measure,
      ordered.source,
    );
    if (appended) {
      return appended;
    }
    return finishFull(ordered, started, measure, "length-change");
  }

  // Uniqueness/item-id eligibility lives on the cache from the last full
  // projection. Do not allocate Sets on the streaming hot path.
  if (!previous.incrementalSafe || !previous.itemIndexById) {
    return finishFull(ordered, started, measure, "duplicate-event-id");
  }

  let dirtyIndex = -1;
  for (let index = 0; index < sortedEvents.length; index += 1) {
    const nextEvent = sortedEvents[index];
    const previousEvent = previous.sortedEvents[index];
    if (nextEvent.id !== previousEvent.id) {
      return finishFull(ordered, started, measure, "id-sequence-change");
    }
    if (
      nextEvent === previousEvent ||
      eventsEqualForProjection(previousEvent, nextEvent)
    ) {
      continue;
    }
    if (dirtyIndex >= 0) {
      return finishFull(ordered, started, measure, "multiple-dirty");
    }
    dirtyIndex = index;
  }

  if (dirtyIndex < 0) {
    return finishMeasured(
      {
        items: previous.items,
        cache: previous,
        mode: "incremental",
        dirtyStart: -1,
        dirtyEnd: -1,
        stableRowReuse: previous.items.length,
        stableRowChurn: 0,
        eventOrder: ordered.source,
      },
      sortedEvents.length,
      started,
      measure,
    );
  }

  const previousEvent = previous.sortedEvents[dirtyIndex];
  const nextEvent = sortedEvents[dirtyIndex];
  const proof = proveBoundedStreamingMutation(previousEvent, nextEvent);
  if (proof) {
    return finishFull(ordered, started, measure, proof);
  }

  const itemIndex = previous.itemIndexById.get(nextEvent.id);
  if (itemIndex == null) {
    return finishFull(ordered, started, measure, "item-missing");
  }
  const previousItem = previous.items[itemIndex];
  if (previousItem.id !== nextEvent.id) {
    return finishFull(ordered, started, measure, "ambiguous");
  }
  if (
    previousItem.type !== "message" &&
    previousItem.type !== "activity"
  ) {
    return finishFull(ordered, started, measure, "ambiguous");
  }

  const nextProjected = buildZenTimelineFromSortedEvents([nextEvent]);
  if (nextProjected.length !== 1) {
    return finishFull(ordered, started, measure, "presence-change");
  }
  const nextItem = nextProjected[0];
  if (
    nextItem.id !== nextEvent.id ||
    nextItem.type !== previousItem.type
  ) {
    return finishFull(ordered, started, measure, "ambiguous");
  }

  const items = previous.items.slice();
  items[itemIndex] = nextItem;
  // Proved path replaces exactly one same-position item — no O(n) identity scan.
  const stableRowReuse = items.length - 1;
  const stableRowChurn = 1;

  return finishMeasured(
    {
      items,
      cache: {
        sortedEvents,
        items,
        // Same-id incremental keeps uniqueness eligibility from the full owner.
        incrementalSafe: previous.incrementalSafe,
        itemIndexById: previous.itemIndexById,
      },
      mode: "incremental",
      dirtyStart: dirtyIndex,
      dirtyEnd: dirtyIndex,
      stableRowReuse,
      stableRowChurn,
      eventOrder: ordered.source,
    },
    sortedEvents.length,
    started,
    measure,
  );
}

/**
 * Proof-gated event order for projection.
 * Prefer the reconciliation contract when raw timestamp + seq + id match the
 * prior sorted cache at each index (exact string/number equality — no
 * Date.parse on the hot path). Changed timestamps fall through to
 * already-sorted verification or a single sort before any full fallback.
 */
export function resolveProjectionEventOrder(
  events: CodexConversationEvent[],
  previous: ZenTimelineProjectionCache | null | undefined,
): {
  sortedEvents: CodexConversationEvent[];
  source: ZenTimelineEventOrderSource;
} {
  if (
    previous &&
    events.length === previous.sortedEvents.length &&
    eventsHaveSameCachedOrderKeys(events, previous.sortedEvents)
  ) {
    // Streaming upserts keep order keys stable while body/partial mutate.
    return { sortedEvents: events.slice(), source: "cached-ids" };
  }
  if (eventsAreSorted(events)) {
    return { sortedEvents: events.slice(), source: "already-sorted" };
  }
  return {
    sortedEvents: events.slice().sort(compareConversationEvents),
    source: "sorted",
  };
}

function finishFull(
  ordered: { sortedEvents: CodexConversationEvent[]; source: ZenTimelineEventOrderSource },
  started: number,
  measure: boolean,
  fallbackReason: TimelineProjectionFallbackReason,
): ZenTimelineProjectionResult {
  const sortedEvents = ordered.sortedEvents;
  const eventOrder = ordered.source;
  // Sorted core — never sort again here.
  const items = buildZenTimelineFromSortedEvents(sortedEvents);
  const safety = computeIncrementalSafety(sortedEvents, items);
  return finishMeasured(
    {
      items,
      cache: {
        sortedEvents,
        items,
        incrementalSafe: safety.incrementalSafe,
        itemIndexById: safety.itemIndexById,
      },
      mode: "full",
      fallbackReason,
      stableRowReuse: 0,
      stableRowChurn: items.length,
      eventOrder,
    },
    sortedEvents.length,
    started,
    measure,
  );
}

function finishMeasured(
  result: ZenTimelineProjectionResult,
  eventCount: number,
  started: number,
  measure: boolean,
): ZenTimelineProjectionResult {
  if (measure) {
    recordTimelineProjectionSample({
      mode: result.mode,
      durationMs: nowMs() - started,
      eventCount,
      itemCount: result.items.length,
      dirtyStart: result.dirtyStart,
      dirtyEnd: result.dirtyEnd,
      fallbackReason: result.fallbackReason,
      stableRowReuse: result.stableRowReuse,
      stableRowChurn: result.stableRowChurn,
    });
  }
  return result;
}

/** One Set scan on canonical full projection only. */
function computeIncrementalSafety(
  sortedEvents: CodexConversationEvent[],
  items: ZenTimelineItem[],
): {
  incrementalSafe: boolean;
  itemIndexById: Map<string, number> | null;
} {
  const seenEventIds = new Set<string>();
  for (const event of sortedEvents) {
    if (seenEventIds.has(event.id)) {
      return { incrementalSafe: false, itemIndexById: null };
    }
    seenEventIds.add(event.id);
  }
  const itemIndexById = new Map<string, number>();
  for (let index = 0; index < items.length; index += 1) {
    const id = items[index].id;
    if (itemIndexById.has(id)) {
      return { incrementalSafe: false, itemIndexById: null };
    }
    itemIndexById.set(id, index);
  }
  return { incrementalSafe: true, itemIndexById };
}

/**
 * Proof-gated single-event append. Reuses every prior timeline item reference
 * when the newest event cannot merge into exploration grouping or mutate a
 * prior command via wait-status attachment.
 */
function tryProvenSingleEventAppend(
  sortedEvents: CodexConversationEvent[],
  previous: ZenTimelineProjectionCache,
  started: number,
  measure: boolean,
  eventOrder: ZenTimelineEventOrderSource,
): ZenTimelineProjectionResult | null {
  if (
    !previous.incrementalSafe ||
    !previous.itemIndexById ||
    sortedEvents.length !== previous.sortedEvents.length + 1
  ) {
    return null;
  }
  for (let index = 0; index < previous.sortedEvents.length; index += 1) {
    const nextEvent = sortedEvents[index];
    const previousEvent = previous.sortedEvents[index];
    if (nextEvent.id !== previousEvent.id) {
      return null;
    }
    if (
      nextEvent !== previousEvent &&
      !eventsEqualForProjection(previousEvent, nextEvent)
    ) {
      return null;
    }
  }
  const newEvent = sortedEvents[sortedEvents.length - 1];
  if (!newEvent || previous.itemIndexById.has(newEvent.id)) {
    return null;
  }
  // Exploration commands contiguous with an open explore:* row merge into that
  // row; single-event projection would incorrectly fork a second card.
  if (
    newEvent.kind === "command" &&
    previous.items[previous.items.length - 1]?.type === "activity" &&
    previous.items[previous.items.length - 1]?.id.startsWith("explore:")
  ) {
    return null;
  }
  const projected = buildZenTimelineFromSortedEvents([newEvent]);
  if (projected.length !== 1) {
    // Empty (wait-status mutation) or multi-row presence is not a pure append.
    return null;
  }
  const newItem = projected[0];
  if (previous.itemIndexById.has(newItem.id)) {
    return null;
  }
  const items = previous.items.concat(newItem);
  const itemIndexById = new Map(previous.itemIndexById);
  itemIndexById.set(newItem.id, items.length - 1);
  return finishMeasured(
    {
      items,
      cache: {
        sortedEvents,
        items,
        incrementalSafe: true,
        itemIndexById,
      },
      mode: "incremental",
      dirtyStart: sortedEvents.length - 1,
      dirtyEnd: sortedEvents.length - 1,
      stableRowReuse: previous.items.length,
      stableRowChurn: 1,
      eventOrder,
    },
    sortedEvents.length,
    started,
    measure,
  );
}

function proveBoundedStreamingMutation(
  previous: CodexConversationEvent,
  next: CodexConversationEvent,
): TimelineProjectionFallbackReason | null {
  if (previous.kind !== next.kind) {
    return "kind-change";
  }
  if (!isIncrementallyProjectableKind(previous.kind)) {
    return "non-message";
  }
  if (previous.timestamp !== next.timestamp) {
    return "timestamp-change";
  }
  if (previous.seq !== next.seq) {
    return "seq-change";
  }
  if (!isBoundedStreamingFieldDelta(previous, next)) {
    return "unbounded-field-delta";
  }
  return null;
}

function isIncrementallyProjectableKind(
  kind: CodexConversationEvent["kind"],
): boolean {
  return (
    kind === "user_message" ||
    kind === "assistant_message" ||
    kind === "tool" ||
    kind === "patch"
  );
}

/**
 * Structural fields must stay fixed. Streaming-mutable fields:
 * - messages: body, partial, status
 * - tools/patches: body, output, partial, status
 */
function isBoundedStreamingFieldDelta(
  previous: CodexConversationEvent,
  next: CodexConversationEvent,
) {
  const toolLike = next.kind === "tool" || next.kind === "patch";
  return (
    previous.id === next.id &&
    previous.kind === next.kind &&
    previous.seq === next.seq &&
    previous.timestamp === next.timestamp &&
    previous.role === next.role &&
    previous.title === next.title &&
    previous.command === next.command &&
    previous.tool_name === next.tool_name &&
    previous.input === next.input &&
    (toolLike || previous.output === next.output) &&
    previous.call_id === next.call_id &&
    previous.exit_code === next.exit_code &&
    previous.transient === next.transient &&
    previous.source === next.source &&
    previous.work_id === next.work_id &&
    previous.work_session_id === next.work_session_id &&
    previous.session_name === next.session_name &&
    previous.unread === next.unread &&
    previous.explanation === next.explanation &&
    sameStringArray(previous.files, next.files) &&
    sameFileChanges(previous.file_changes, next.file_changes) &&
    samePlan(previous.plan, next.plan) &&
    true
  );
}

function eventsEqualForProjection(
  left: CodexConversationEvent,
  right: CodexConversationEvent,
) {
  return (
    isBoundedStreamingFieldDelta(left, right) &&
    left.body === right.body &&
    left.partial === right.partial &&
    left.status === right.status
  );
}

function sameStringArray(left?: string[], right?: string[]) {
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

function sameFileChanges(
  left: CodexConversationEvent["file_changes"],
  right: CodexConversationEvent["file_changes"],
) {
  if (left === right) {
    return true;
  }
  if (!left || !right || left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    const a = left[index];
    const b = right[index];
    if (
      a.path !== b.path ||
      a.move_path !== b.move_path ||
      a.operation !== b.operation ||
      a.additions !== b.additions ||
      a.deletions !== b.deletions
    ) {
      return false;
    }
  }
  return true;
}

function samePlan(
  left: CodexConversationEvent["plan"],
  right: CodexConversationEvent["plan"],
) {
  if (left === right) {
    return true;
  }
  if (!left || !right || left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (
      left[index]?.step !== right[index]?.step ||
      left[index]?.status !== right[index]?.status
    ) {
      return false;
    }
  }
  return true;
}

function eventsHaveSameCachedOrderKeys(
  left: CodexConversationEvent[],
  right: CodexConversationEvent[],
) {
  for (let index = 0; index < left.length; index += 1) {
    if (!conversationEventCachedOrderKeysEqual(left[index], right[index])) {
      return false;
    }
  }
  return true;
}

/**
 * Sufficient cached-order proof: exact raw timestamp + seq + id.
 * Avoids parsing timestamps on every streaming token.
 */
function conversationEventCachedOrderKeysEqual(
  left: CodexConversationEvent,
  right: CodexConversationEvent,
) {
  return (
    left.id === right.id &&
    left.seq === right.seq &&
    left.timestamp === right.timestamp
  );
}

function eventsAreSorted(events: CodexConversationEvent[]) {
  for (let index = 1; index < events.length; index += 1) {
    if (compareConversationEvents(events[index - 1], events[index]) > 0) {
      return false;
    }
  }
  return true;
}

function nowMs() {
  const perf = (
    globalThis as { performance?: { now(): number } }
  ).performance;
  return perf?.now?.() ?? Date.now();
}
