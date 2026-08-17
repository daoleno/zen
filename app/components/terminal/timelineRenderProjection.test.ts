import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../../services/codexConversation";
import { buildZenTimeline } from "./InterfaceTimelineModel";
import {
  buildTimelineRenderItems,
  projectTimelineRenderItems,
  type TimelineRenderItem,
} from "./InterfaceTimelineGrouping";
import { makeMixedTimelineEvents } from "./timelineProjectionFixtures";
import { projectZenTimeline } from "./projectZenTimeline";

function canonical(items: ReturnType<typeof buildZenTimeline>) {
  return buildTimelineRenderItems(items.slice().reverse(), {
    showDateDividers: true,
  });
}

function renderSignature(items: TimelineRenderItem[]) {
  return items.map((item) =>
    item.type === "message"
      ? {
          id: item.id,
          type: item.type,
          role: item.role,
          body: item.body,
          presentation: item.presentation,
        }
      : item.type === "date-divider"
        ? { id: item.id, type: item.type, label: item.label }
        : { id: item.id, type: item.type },
  );
}

function message(
  id: string,
  seq: number,
  timestamp: string,
  body = id,
): CodexConversationEvent {
  return {
    id,
    seq,
    timestamp,
    kind: seq % 2 === 0 ? "user_message" : "assistant_message",
    role: seq % 2 === 0 ? "user" : "assistant",
    body,
  };
}

describe("timeline render projection", () => {
  test.each([1_000, 5_000, 10_000])(
    "same-id stream update at %,d items changes one render row and never empties",
    (count) => {
      const events = makeMixedTimelineEvents(count);
      const initialTimeline = projectZenTimeline(events, null);
      const initialItems = initialTimeline.items;
      const initial = projectTimelineRenderItems(
        initialItems,
        { showDateDividers: true },
        null,
      );
      const changedEvents = events.slice();
      const changedIndex = changedEvents.findIndex(
        (event) => event.kind === "assistant_message",
      );
      changedEvents[changedIndex] = {
        ...changedEvents[changedIndex]!,
        body: `${changedEvents[changedIndex]!.body}\n\n## Streaming\n\n\`\`\`ts\nconst longHistory = true;\n\`\`\``,
        partial: true,
      };
      const nextTimeline = projectZenTimeline(
        changedEvents,
        initialTimeline.cache,
      );
      const nextItems = nextTimeline.items;
      const next = projectTimelineRenderItems(
        nextItems,
        { showDateDividers: true },
        initial.cache,
      );

      expect(next.mode).toBe("update");
      expect(next.items.length).toBeGreaterThan(0);
      expect(renderSignature(next.items)).toEqual(
        renderSignature(canonical(nextItems)),
      );
      const changedReferences = next.items.filter(
        (item, index) => item !== initial.items[index],
      );
      expect(changedReferences).toHaveLength(1);
      expect(next.items.map((item) => item.id)).toEqual(
        initial.items.map((item) => item.id),
      );
    },
  );

  test("newest append rebuilds only the newest boundary day", () => {
    const initialEvents = [
      message("old-a", 0, "2026-08-15T10:00:00.000Z"),
      message("old-b", 1, "2026-08-16T10:00:00.000Z"),
      message("edge", 2, "2026-08-16T11:00:00.000Z"),
    ];
    const initialTimeline = projectZenTimeline(initialEvents, null);
    const initialItems = initialTimeline.items;
    const initial = projectTimelineRenderItems(
      initialItems,
      { showDateDividers: true },
      null,
    );
    const nextTimeline = projectZenTimeline([
      ...initialEvents,
      message("new", 3, "2026-08-17T12:00:00.000Z"),
    ], initialTimeline.cache);
    const nextItems = nextTimeline.items;
    const next = projectTimelineRenderItems(
      nextItems,
      { showDateDividers: true },
      initial.cache,
    );

    expect(next.mode).toBe("append");
    expect(renderSignature(next.items)).toEqual(
      renderSignature(canonical(nextItems)),
    );
    expect(next.items.find((item) => item.id === "old-a")).toBe(
      initial.items.find((item) => item.id === "old-a"),
    );
  });

  test("oldest prepend preserves the live-edge render prefix", () => {
    const oldEvents = [
      message("edge-a", 10, "2026-08-16T10:00:00.000Z"),
      message("edge-b", 11, "2026-08-17T10:00:00.000Z"),
    ];
    const initialTimeline = projectZenTimeline(oldEvents, null);
    const initialItems = initialTimeline.items;
    const initial = projectTimelineRenderItems(
      initialItems,
      { showDateDividers: true },
      null,
    );
    const nextTimeline = projectZenTimeline([
      message("history-a", 1, "2026-08-14T10:00:00.000Z"),
      message("history-b", 2, "2026-08-15T10:00:00.000Z"),
      ...oldEvents,
    ], initialTimeline.cache);
    const nextItems = nextTimeline.items;
    const next = projectTimelineRenderItems(
      nextItems,
      { showDateDividers: true },
      initial.cache,
    );

    expect(next.mode).toBe("prepend");
    expect(renderSignature(next.items)).toEqual(
      renderSignature(canonical(nextItems)),
    );
    expect(next.items[0]).toBe(initial.items[0]);
  });

  test("topology changes fall back to canonical full projection", () => {
    const initialItems = buildZenTimeline([
      message("a", 0, "2026-08-17T10:00:00.000Z"),
      message("b", 1, "2026-08-17T10:01:00.000Z"),
    ]);
    const initial = projectTimelineRenderItems(
      initialItems,
      { showDateDividers: true },
      null,
    );
    const changed = initialItems.map((item) =>
      item.id === "b" ? { ...item, timestamp: "2026-08-16T10:01:00.000Z" } : item,
    );
    const next = projectTimelineRenderItems(
      changed,
      { showDateDividers: true },
      initial.cache,
    );

    expect(next.mode).toBe("full");
    expect(renderSignature(next.items)).toEqual(
      renderSignature(canonical(changed)),
    );
  });
});
