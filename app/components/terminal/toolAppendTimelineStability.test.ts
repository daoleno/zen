import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../../services/codexConversation";
import {
  buildTimelineRenderItems,
  stabilizeTimelineRenderItems,
  type TimelineRenderItem,
} from "./InterfaceTimelineGrouping";
import type { ZenTimelineItem } from "./InterfaceTimelineItemView";
import { projectZenTimeline } from "./projectZenTimeline";
import { makeMixedTimelineEvents } from "./timelineProjectionFixtures";
import { timelineItemsSemanticEqual } from "./timelineItemsSemanticEqual";

/**
 * Distinguishes four invalidation layers on a long mixed timeline when a new
 * tool/activity row arrives:
 * 1. raw ZenTimelineItem identity from projectZenTimeline
 * 2. grouped TimelineRenderItem identity after stabilize
 * 3. row memo props (item + presentation references)
 * 4. FlatList data-array identity (new array allowed on length change)
 *
 * Historical rows must keep (1)-(3). Only the new/active tool row and
 * legitimate adjacent message-grouping boundaries may change.
 */

const NEW_TOOL_ID = "tool-append-live";

function appendRunningTool(
  events: CodexConversationEvent[],
): CodexConversationEvent[] {
  const last = events[events.length - 1];
  const lastMs = last?.timestamp
    ? Date.parse(last.timestamp)
    : Date.parse("2026-08-06T00:00:00.000Z");
  return [
    ...events,
    {
      id: NEW_TOOL_ID,
      seq: (last?.seq ?? events.length) + 1,
      kind: "tool",
      tool_name: "Grep",
      input: `{"pattern":"live-append","path":"app"}`,
      body: "live match",
      status: "running",
      partial: true,
      timestamp: new Date(lastMs + 60_000).toISOString(),
    },
  ];
}

function upsertToolBody(
  events: CodexConversationEvent[],
  toolId: string,
  body: string,
): CodexConversationEvent[] {
  return events.map((event) =>
    event.id === toolId
      ? { ...event, body, status: "running", partial: true }
      : event,
  );
}

function historicalIds(items: { id: string }[], activeId: string) {
  return items.map((item) => item.id).filter((id) => id !== activeId);
}

function indexById<T extends { id: string }>(items: T[]) {
  return new Map(items.map((item) => [item.id, item]));
}

function memoPropsOf(item: TimelineRenderItem) {
  if (item.type === "date-divider") {
    return { item, presentation: undefined };
  }
  return {
    item: item.type === "message" ? (item.sourceItem ?? item) : item,
    presentation: item.type === "message" ? item.presentation : undefined,
  };
}

describe("tool/activity append timeline stability layers", () => {
  test("long mixed timeline: new tool preserves historical raw, grouped, and memo identities", () => {
    const baseEvents = makeMixedTimelineEvents(120);
    const initial = projectZenTimeline(baseEvents, null);
    expect(initial.mode).toBe("full");
    expect(initial.items.length).toBeGreaterThanOrEqual(110);

    const nextEvents = appendRunningTool(baseEvents);
    const projected = projectZenTimeline(nextEvents, initial.cache);

    // Layer 1 — authoritative projection must keep historical raw item refs.
    expect(projected.mode).toBe("incremental");
    expect(projected.fallbackReason).toBeUndefined();
    const previousRaw = indexById(initial.items);
    const historicalRawIds = historicalIds(projected.items, NEW_TOOL_ID);
    expect(historicalRawIds.length).toBe(initial.items.length);
    for (const id of historicalRawIds) {
      expect(projected.items.find((item) => item.id === id)).toBe(
        previousRaw.get(id),
      );
    }
    expect(projected.items.some((item) => item.id === NEW_TOOL_ID)).toBe(true);
    expect(projected.stableRowChurn).toBeLessThanOrEqual(2);
    expect(projected.stableRowReuse).toBeGreaterThanOrEqual(
      initial.items.length - 1,
    );

    // Salvage mirror of useInterfaceTimelineItems (should be identity no-op when
    // projection already preserved refs).
    const salvaged: ZenTimelineItem[] = projected.items.map((item) => {
      const prior = previousRaw.get(item.id);
      return prior && timelineItemsSemanticEqual(prior, item) ? prior : item;
    });
    for (const id of historicalRawIds) {
      expect(salvaged.find((item) => item.id === id)).toBe(previousRaw.get(id));
    }

    // Layer 2 — grouped render items.
    const previousRender = buildTimelineRenderItems([...initial.items].reverse(), {
      showDateDividers: true,
    });
    const nextRender = buildTimelineRenderItems([...salvaged].reverse(), {
      showDateDividers: true,
    });
    const stableRender = stabilizeTimelineRenderItems(
      previousRender,
      nextRender,
    );
    const previousGrouped = indexById(previousRender);
    for (const id of historicalRawIds) {
      const nextItem = stableRender.find((item) => item.id === id);
      const priorItem = previousGrouped.get(id);
      expect(nextItem).toBeDefined();
      expect(priorItem).toBeDefined();
      // Adjacent message grouping at the newest edge may change; everything
      // else must keep the prior grouped object.
      if (
        priorItem?.type === "message" &&
        nextItem?.type === "message" &&
        priorItem.presentation?.groupPosition !==
          nextItem.presentation?.groupPosition
      ) {
        const priorIndex = previousRender.findIndex((item) => item.id === id);
        const nextIndex = stableRender.findIndex((item) => item.id === id);
        // Only the chronological neighbor of the new tool may legitimately
        // change grouping; that neighbor sits next to NEW_TOOL_ID in the
        // inverted list (index 1 when tool is newest at index 0).
        expect(nextIndex).toBeLessThanOrEqual(2);
        expect(priorIndex).toBeLessThanOrEqual(2);
        continue;
      }
      expect(nextItem).toBe(priorItem);
    }

    // Layer 3 — row memo props (item + presentation) for historical rows.
    for (const id of historicalRawIds) {
      const nextItem = stableRender.find((item) => item.id === id);
      const priorItem = previousGrouped.get(id);
      if (!nextItem || !priorItem) {
        throw new Error(`missing render item ${id}`);
      }
      if (nextItem !== priorItem) {
        // Legitimate adjacent grouping change only.
        continue;
      }
      const nextMemo = memoPropsOf(nextItem);
      const priorMemo = memoPropsOf(priorItem);
      expect(nextMemo.item).toBe(priorMemo.item);
      expect(nextMemo.presentation).toBe(priorMemo.presentation);
    }

    // Layer 4 — FlatList data identity may change on length growth, but every
    // historical cell key must still resolve to the prior element reference
    // (no blanket remount via new row objects).
    expect(stableRender).not.toBe(previousRender);
    expect(stableRender.length).toBeGreaterThan(previousRender.length);
    let stableHistoricalCells = 0;
    for (const id of historicalRawIds) {
      if (
        stableRender.find((item) => item.id === id) === previousGrouped.get(id)
      ) {
        stableHistoricalCells += 1;
      }
    }
    expect(stableHistoricalCells).toBeGreaterThanOrEqual(
      historicalRawIds.length - 2,
    );
  });

  test("same-id tool activity upsert preserves unrelated historical raw identities", () => {
    const baseEvents = makeMixedTimelineEvents(120);
    const toolEvent = baseEvents.find((event) => event.kind === "tool");
    if (!toolEvent) {
      throw new Error("fixture requires a tool event");
    }
    const initial = projectZenTimeline(baseEvents, null);
    const nextEvents = upsertToolBody(
      baseEvents,
      toolEvent.id,
      `${toolEvent.body}\nsecond match`,
    );
    const projected = projectZenTimeline(nextEvents, initial.cache);

    expect(projected.mode).toBe("incremental");
    expect(projected.fallbackReason).toBeUndefined();
    expect(projected.stableRowChurn).toBe(1);

    const previousRaw = indexById(initial.items);
    for (const item of projected.items) {
      if (item.id === toolEvent.id) {
        expect(item).not.toBe(previousRaw.get(item.id));
        continue;
      }
      const previous = previousRaw.get(item.id);
      if (!previous) {
        throw new Error(`missing previous raw row ${item.id}`);
      }
      expect(item).toBe(previous);
    }
  });
});
