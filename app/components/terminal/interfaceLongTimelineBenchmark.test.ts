import { expect, test } from "bun:test";
import type { CodexConversationEvent } from "../../services/codexConversation";
import { reconcileConversationDeltaEvents } from "./interfaceConversationReconciliation";
import { projectTimelineRenderItems } from "./InterfaceTimelineGrouping";
import { makeComplexTimelineEvents, lastAssistantEventId } from "./timelineProjectionFixtures";
import { projectZenTimeline } from "./projectZenTimeline";

const benchmark =
  process.env.ZEN_INTERFACE_PERF_BENCH === "1" ? test : test.skip;
const COUNTS = [1_000, 5_000, 10_000] as const;
const STREAM_ITERATIONS = 40;
const APPEND_ITERATIONS = 20;
const PREPEND_ITERATIONS = 10;
const PREPEND_PAGE_SIZE = 20;

function nowMs() {
  return Bun.nanoseconds() / 1e6;
}

function summarize(values: number[]) {
  const sorted = values.slice().sort((left, right) => left - right);
  const at = (percentile: number) =>
    sorted[Math.max(0, Math.ceil(sorted.length * percentile) - 1)] ?? 0;
  return {
    n: sorted.length,
    p50: at(0.5),
    p95: at(0.95),
    max: sorted[sorted.length - 1] ?? 0,
    over16_7ms: sorted.filter((value) => value > 16.7).length,
    over50ms: sorted.filter((value) => value > 50).length,
  };
}

benchmark("Interface 1k/5k/10k production-owner performance", () => {
  const output = [];
  for (const count of COUNTS) {
    const fixture = makeComplexTimelineEvents(count);
    const initialDurations: number[] = [];
    for (let iteration = 0; iteration < 10; iteration += 1) {
      const started = nowMs();
      const timeline = projectZenTimeline(fixture, null);
      const render = projectTimelineRenderItems(
        timeline.items,
        { showDateDividers: true },
        null,
      );
      initialDurations.push(nowMs() - started);
      expect(render.items.length).toBeGreaterThan(0);
    }

    let events = fixture;
    let timeline = projectZenTimeline(events, null);
    let render = projectTimelineRenderItems(
      timeline.items,
      { showDateDividers: true },
      null,
    );
    const streamEventId = lastAssistantEventId(events);
    const streamDurations: number[] = [];
    let streamChangedSourceMax = 0;
    let streamRenderChurnMax = 0;
    for (let iteration = 0; iteration < STREAM_ITERATIONS; iteration += 1) {
      const current = events.find((event) => event.id === streamEventId)!;
      const upsert: CodexConversationEvent = {
        ...current,
        body: `${current.body ?? ""}\n\n## Stream ${iteration}\n\n\`\`\`ts\nconst revision = ${iteration};\n\`\`\``,
        partial: true,
      };
      const started = nowMs();
      events = reconcileConversationDeltaEvents(events, [upsert]);
      timeline = projectZenTimeline(events, timeline.cache);
      render = projectTimelineRenderItems(
        timeline.items,
        { showDateDividers: true },
        render.cache,
      );
      streamDurations.push(nowMs() - started);
      streamChangedSourceMax = Math.max(
        streamChangedSourceMax,
        render.changedSourceCount,
      );
      streamRenderChurnMax = Math.max(
        streamRenderChurnMax,
        render.stableRenderChurn,
      );
      expect(render.items.length).toBeGreaterThan(0);
    }

    const appendDurations: number[] = [];
    for (let iteration = 0; iteration < APPEND_ITERATIONS; iteration += 1) {
      const tail = events[events.length - 1]!;
      const appended: CodexConversationEvent = {
        id: `benchmark-append-${count}-${iteration}`,
        seq: tail.seq + 1,
        timestamp: new Date(
          Date.parse(tail.timestamp ?? "2026-08-17T00:00:00.000Z") + 60_000,
        ).toISOString(),
        kind: "assistant_message",
        role: "assistant",
        body: `Append ${iteration}\n\n\`\`\`ts\nconst appended = true;\n\`\`\``,
      };
      const nextEvents = events.concat(appended);
      const started = nowMs();
      timeline = projectZenTimeline(nextEvents, timeline.cache);
      render = projectTimelineRenderItems(
        timeline.items,
        { showDateDividers: true },
        render.cache,
      );
      appendDurations.push(nowMs() - started);
      events = nextEvents;
      expect(render.mode).toBe("append");
    }

    const prependDurations: number[] = [];
    for (let page = 0; page < PREPEND_ITERATIONS; page += 1) {
      const oldest = events[0]!;
      const oldestTime = Date.parse(
        oldest.timestamp ?? "2026-08-17T00:00:00.000Z",
      );
      const older = Array.from(
        { length: PREPEND_PAGE_SIZE },
        (_, index): CodexConversationEvent => ({
          id: `benchmark-prepend-${count}-${page}-${index}`,
          seq: oldest.seq - PREPEND_PAGE_SIZE + index,
          timestamp: new Date(
            oldestTime - (PREPEND_PAGE_SIZE - index) * 60_000,
          ).toISOString(),
          kind: index % 2 === 0 ? "user_message" : "assistant_message",
          role: index % 2 === 0 ? "user" : "assistant",
          body: `Historical page ${page} row ${index}`,
        }),
      );
      const nextEvents = older.concat(events);
      const started = nowMs();
      timeline = projectZenTimeline(nextEvents, timeline.cache);
      render = projectTimelineRenderItems(
        timeline.items,
        { showDateDividers: true },
        render.cache,
      );
      prependDurations.push(nowMs() - started);
      events = nextEvents;
      expect(render.mode).toBe("prepend");
    }

    output.push({
      itemCount: count,
      samples: {
        initial: summarize(initialDurations),
        streamChunk: summarize(streamDurations),
        append: summarize(appendDurations),
        prependPage20: summarize(prependDurations),
      },
      streamChangedSourceMax,
      streamRenderChurnMax,
      blankWindowObservedInPureDataHarness: false,
      blankWindowMeasurement: "device onViewableItemsChanged collector",
    });
  }
  console.log(`zen-interface-long-history-benchmark ${JSON.stringify(output)}`);
});
