import { afterEach, describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../../services/codexConversation";
import { buildZenTimeline } from "./InterfaceTimelineModel";
import { prepareInterfaceMarkdown } from "./InterfaceNativeMarkdownBodyPrepare";
import {
  projectZenTimeline,
  resolveProjectionEventOrder,
  type ZenTimelineProjectionCache,
} from "./projectZenTimeline";
import {
  disableTimelineProjectionPerf,
  enableTimelineProjectionPerf,
  evaluateTimelineProjectionPerfCollectionAllowed,
  getTimelineProjectionPerfSnapshot,
  isTimelineProjectionPerfEnabled,
  resetTimelineProjectionPerf,
  sumMarkdownPrepareDurationMs,
  timelineProjectionPerfCollectionAllowed,
  type TimelineProjectionFallbackReason,
} from "./timelineProjectionPerf";
import {
  firstAssistantEventId,
  makeMixedTimelineEvents,
  withAssistantBodyRevision,
} from "./timelineProjectionFixtures";
import type { ZenTimelineItem } from "./InterfaceTimelineItemView";

afterEach(() => {
  disableTimelineProjectionPerf();
  resetTimelineProjectionPerf();
});

describe("projectZenTimeline incremental projection", () => {
  test("500-event streaming upserts stay under 20% of full projection CPU with stable settled rows", () => {
    const base = makeMixedTimelineEvents(500);
    const streamId = firstAssistantEventId(base);
    const revisions = 100;
    // Precompute outside timed regions — array construction is not projection CPU.
    const revisionEvents = Array.from({ length: revisions + 1 }, (_, revision) =>
      withAssistantBodyRevision(base, streamId, revision),
    );

    enableTimelineProjectionPerf();
    try {
      // Shared warmup so cold JIT does not dominate either path.
      let warmCache: ZenTimelineProjectionCache | null = null;
      for (let revision = 0; revision < 10; revision += 1) {
        warmCache = projectZenTimeline(
          revisionEvents[revision],
          warmCache,
        ).cache;
      }

      let cache: ZenTimelineProjectionCache | null = null;
      let minStableReuse = Number.POSITIVE_INFINITY;
      for (let revision = 0; revision < revisions; revision += 1) {
        const result = projectZenTimeline(revisionEvents[revision], cache);
        cache = result.cache;
        if (revision === 0) {
          expect(result.mode).toBe("full");
          expect(result.cache.incrementalSafe).toBe(true);
          expect(result.items.length).toBeGreaterThanOrEqual(499);
          continue;
        }
        expect(result.mode).toBe("incremental");
        expect(result.fallbackReason).toBeUndefined();
        expect(result.eventOrder).toBe("cached-ids");
        expect(result.cache.incrementalSafe).toBe(true);
        minStableReuse = Math.min(minStableReuse, result.stableRowReuse);
        expect(result.stableRowChurn).toBe(1);
      }
      expect(minStableReuse).toBeGreaterThanOrEqual(498);

      const ratios: number[] = [];
      for (let trial = 0; trial < 3; trial += 1) {
        const fullStart = nowMs();
        for (let revision = 1; revision <= revisions; revision += 1) {
          projectZenTimeline(revisionEvents[revision], null);
        }
        const fullWallMs = nowMs() - fullStart;

        let rolling: ZenTimelineProjectionCache | null = projectZenTimeline(
          revisionEvents[0],
          null,
        ).cache;
        const incrementalStart = nowMs();
        for (let revision = 1; revision <= revisions; revision += 1) {
          rolling = projectZenTimeline(revisionEvents[revision], rolling).cache;
        }
        const incrementalWallMs = nowMs() - incrementalStart;
        expect(fullWallMs).toBeGreaterThan(0);
        ratios.push(incrementalWallMs / fullWallMs);
      }
      ratios.sort((left, right) => left - right);
      const medianRatio = ratios[1];
      // Report measured median ratio truthfully (not gamed).
      console.log(
        JSON.stringify({
          benchmark: "500-event-streaming-projection",
          trials: ratios,
          medianRatio,
        }),
      );
      expect(medianRatio).toBeLessThanOrEqual(0.2);
    } finally {
      disableTimelineProjectionPerf();
      resetTimelineProjectionPerf();
    }
  });

  test("incremental output matches canonical full projection across property cases", () => {
    const cases: Array<{
      name: string;
      previous: CodexConversationEvent[];
      next: CodexConversationEvent[];
      expectMode: "full" | "incremental";
      fallbackReason?: TimelineProjectionFallbackReason;
    }> = [];

    const mixed50 = makeMixedTimelineEvents(50);
    const streamId = firstAssistantEventId(mixed50);

    cases.push({
      name: "text-growth",
      previous: withAssistantBodyRevision(mixed50, streamId, 1),
      next: withAssistantBodyRevision(mixed50, streamId, 2),
      expectMode: "incremental",
    });
    cases.push({
      name: "text-shrink",
      previous: withAssistantBodyRevision(mixed50, streamId, 8),
      next: withAssistantBodyRevision(mixed50, streamId, 1),
      expectMode: "incremental",
    });

    const equalTs: CodexConversationEvent[] = [
      {
        id: "a",
        seq: 1,
        kind: "user_message",
        role: "user",
        body: "one",
        timestamp: "2026-08-06T12:00:00.000Z",
      },
      {
        id: "b",
        seq: 2,
        kind: "assistant_message",
        role: "assistant",
        body: "two",
        timestamp: "2026-08-06T12:00:00.000Z",
        partial: true,
      },
    ];
    cases.push({
      name: "equal-timestamps",
      previous: equalTs,
      next: equalTs.map((event) =>
        event.id === "b" ? { ...event, body: "two!" } : event,
      ),
      expectMode: "incremental",
    });

    const dateBoundary: CodexConversationEvent[] = [
      {
        id: "day1-user",
        seq: 1,
        kind: "user_message",
        role: "user",
        body: "yesterday",
        timestamp: "2026-08-05T23:00:00.000Z",
      },
      {
        id: "day2-assistant",
        seq: 2,
        kind: "assistant_message",
        role: "assistant",
        body: "today",
        timestamp: "2026-08-06T01:00:00.000Z",
        partial: true,
      },
    ];
    cases.push({
      name: "date-boundary-stream",
      previous: dateBoundary,
      next: dateBoundary.map((event) =>
        event.id === "day2-assistant"
          ? { ...event, body: "today continues" }
          : event,
      ),
      expectMode: "incremental",
    });

    const withExploration: CodexConversationEvent[] = [
      {
        id: "u1",
        seq: 1,
        kind: "user_message",
        role: "user",
        body: "look",
        timestamp: "2026-08-06T02:00:00.000Z",
      },
      {
        id: "c1",
        seq: 2,
        kind: "command",
        command: "ls app",
        status: "done",
        body: "a.ts",
        timestamp: "2026-08-06T02:01:00.000Z",
      },
      {
        id: "t1",
        seq: 3,
        kind: "tool",
        tool_name: "Grep",
        input: `{"pattern":"x"}`,
        body: "hit",
        status: "done",
        timestamp: "2026-08-06T02:02:00.000Z",
      },
      {
        id: "a1",
        seq: 4,
        kind: "assistant_message",
        role: "assistant",
        body: "found",
        timestamp: "2026-08-06T02:03:00.000Z",
        partial: true,
      },
    ];
    cases.push({
      name: "exploration-tool-adjacency",
      previous: withExploration,
      next: withExploration.map((event) =>
        event.id === "a1" ? { ...event, body: "found more" } : event,
      ),
      expectMode: "incremental",
    });

    const withWork: CodexConversationEvent[] = [
      {
        id: "work-1",
        seq: 1,
        kind: "status",
        source: "work_result",
        status: "session.done",
        work_id: "w1",
        title: "Ship",
        body: "done",
        timestamp: "2026-08-06T03:00:00.000Z",
        work_review_state: "resolved",
        work_session_state: "not_required",
        work_result_current: true,
      },
      {
        id: "assist-work",
        seq: 2,
        kind: "assistant_message",
        role: "assistant",
        body: "ok",
        timestamp: "2026-08-06T03:01:00.000Z",
        partial: true,
      },
    ];
    cases.push({
      name: "typed-work-card-adjacent",
      previous: withWork,
      next: withWork.map((event) =>
        event.id === "assist-work" ? { ...event, body: "ok!" } : event,
      ),
      expectMode: "incremental",
    });

    cases.push({
      name: "middle-insert-falls-back",
      previous: mixed50.slice(0, 10),
      next: [
        ...mixed50.slice(0, 5),
        {
          id: "inserted-mid",
          seq: 10_000,
          kind: "assistant_message",
          role: "assistant",
          body: "mid",
          timestamp: "2026-08-06T00:04:30.000Z",
        },
        ...mixed50.slice(5, 10),
      ],
      expectMode: "full",
      fallbackReason: "length-change",
    });

    cases.push({
      name: "pure-tail-append-stays-incremental",
      previous: mixed50.slice(0, 10),
      next: mixed50.slice(0, 11),
      expectMode: "incremental",
    });

    cases.push({
      name: "delete-falls-back",
      previous: mixed50.slice(0, 11),
      next: mixed50.slice(0, 10),
      expectMode: "full",
      fallbackReason: "length-change",
    });

    const reordered = mixed50.slice(0, 8);
    cases.push({
      name: "reorder-falls-back",
      previous: reordered,
      next: reordered.map((event, index) =>
        index === 3
          ? { ...event, id: `replaced-${event.id}` }
          : event,
      ),
      expectMode: "full",
      fallbackReason: "id-sequence-change",
    });

    cases.push({
      name: "snapshot-length-change",
      previous: makeMixedTimelineEvents(20),
      next: makeMixedTimelineEvents(5),
      expectMode: "full",
      fallbackReason: "length-change",
    });

    cases.push({
      name: "tool-body-change-stays-incremental",
      previous: withExploration,
      next: withExploration.map((event) =>
        event.id === "t1" ? { ...event, body: "hit\nmore" } : event,
      ),
      expectMode: "incremental",
    });

    cases.push({
      name: "empty-to-visible-presence-fallback",
      previous: [
        {
          id: "empty-assist",
          seq: 1,
          kind: "assistant_message",
          role: "assistant",
          body: "   ",
          timestamp: "2026-08-06T04:00:00.000Z",
          partial: true,
        },
        {
          id: "user-keep",
          seq: 2,
          kind: "user_message",
          role: "user",
          body: "hi",
          timestamp: "2026-08-06T04:01:00.000Z",
        },
      ],
      next: [
        {
          id: "empty-assist",
          seq: 1,
          kind: "assistant_message",
          role: "assistant",
          body: "now visible",
          timestamp: "2026-08-06T04:00:00.000Z",
          partial: true,
        },
        {
          id: "user-keep",
          seq: 2,
          kind: "user_message",
          role: "user",
          body: "hi",
          timestamp: "2026-08-06T04:01:00.000Z",
        },
      ],
      expectMode: "full",
      fallbackReason: "item-missing",
    });

    for (const testCase of cases) {
      const primed = projectZenTimeline(testCase.previous, null);
      const optimized = projectZenTimeline(testCase.next, primed.cache);
      const canonical = buildZenTimeline(testCase.next);
      expect(optimized.mode, testCase.name).toBe(testCase.expectMode);
      if (testCase.fallbackReason) {
        expect(optimized.fallbackReason, testCase.name).toBe(
          testCase.fallbackReason,
        );
      }
      expect(
        timelineItemsSemanticallyEqual(optimized.items, canonical),
        testCase.name,
      ).toBe(true);
    }
  });

  test("50 and 500 fixtures project deterministically", () => {
    const fifty = makeMixedTimelineEvents(50);
    const fiveHundred = makeMixedTimelineEvents(500);
    expect(buildZenTimeline(fifty).map((item) => item.id)).toEqual(
      projectZenTimeline(fifty, null).items.map((item) => item.id),
    );
    expect(buildZenTimeline(fiveHundred).length).toBeGreaterThan(400);
    expect(projectZenTimeline(fiveHundred, null).items.length).toEqual(
      buildZenTimeline(fiveHundred).length,
    );
  });

  test("markdown prepare instrumentation records durations without bodies", () => {
    enableTimelineProjectionPerf();
    try {
      resetTimelineProjectionPerf();
      prepareInterfaceMarkdown("hello **world** and more text", true);
      const snapshot = getTimelineProjectionPerfSnapshot();
      expect(snapshot.markdownPrepares.length).toBe(1);
      expect(snapshot.markdownPrepares[0]?.durationMs).toBeGreaterThanOrEqual(0);
      expect(snapshot.markdownPrepares[0]?.inputChars).toBeGreaterThan(0);
      expect(JSON.stringify(snapshot)).not.toContain("hello **world**");
      expect(sumMarkdownPrepareDurationMs()).toBeGreaterThanOrEqual(0);
    } finally {
      disableTimelineProjectionPerf();
      resetTimelineProjectionPerf();
    }
  });
});

describe("mid-review projection contracts", () => {
  test("full/structural path sorts at most once via the sorted-events core", async () => {
    const modelSource = await Bun.file(
      new URL("./InterfaceTimelineModel.ts", import.meta.url),
    ).text();
    const projectorSource = await Bun.file(
      new URL("./projectZenTimeline.ts", import.meta.url),
    ).text();

    expect(modelSource).toContain(
      "export function buildZenTimelineFromSortedEvents(",
    );
    expect(modelSource).toContain(
      "return buildZenTimelineFromSortedEvents(\n    events.slice().sort(compareConversationEvents),",
    );
    expect(modelSource).toContain("for (const event of sortedEvents)");
    const sortedCore = sourceBetween(
      modelSource,
      "export function buildZenTimelineFromSortedEvents(",
      "\nfunction attachWaitStatusToLastCommand(",
    );
    expect(sortedCore).not.toContain(".sort(");
    expect(sortedCore).not.toContain("compareConversationEvents");

    expect(projectorSource).toContain("buildZenTimelineFromSortedEvents");
    expect(projectorSource).toContain(
      "const items = buildZenTimelineFromSortedEvents(sortedEvents);",
    );
    // Projector must not call the sorting wrapper on the full path.
    expect(projectorSource).not.toMatch(
      /buildZenTimeline\s*\(\s*sortedEvents/,
    );
    expect(projectorSource).not.toContain('import { buildZenTimeline }');

    const shuffled = [...makeMixedTimelineEvents(40)].reverse();
    const full = projectZenTimeline(shuffled, null);
    expect(full.mode).toBe("full");
    expect(full.eventOrder).toBe("sorted");
    expect(
      timelineItemsSemanticallyEqual(full.items, buildZenTimeline(shuffled)),
    ).toBe(true);
  });

  test("projector never accepts aliases; turn-focus stays hook-owned", async () => {
    const projectorSource = await Bun.file(
      new URL("./projectZenTimeline.ts", import.meta.url),
    ).text();
    const hookSource = await Bun.file(
      new URL("./useInterfaceTimelineItems.ts", import.meta.url),
    ).text();

    expect(projectorSource).not.toContain("turnFocusAnchorAliases");
    expect(projectorSource).toContain(
      "this API never accepts or caches aliases",
    );
    expect(projectZenTimeline.length).toBe(2);

    const events: CodexConversationEvent[] = [
      {
        id: "provider-user-9",
        seq: 9,
        kind: "user_message",
        role: "user",
        body: "provider canonical body",
        timestamp: "2026-08-06T05:00:00.000Z",
      },
    ];
    const projected = projectZenTimeline(events, null);
    expect(projected.items[0]).toMatchObject({
      id: "provider-user-9",
      body: "provider canonical body",
    });
    expect(
      (projected.items[0] as { turnFocusAnchorId?: string }).turnFocusAnchorId,
    ).toBeUndefined();

    // Canonical build with aliases still works for tests that need it; hook
    // applies aliases after projection so cache cannot retain stale maps.
    const withAlias = buildZenTimeline(
      events,
      new Map([["provider-user-9", "pending-current"]]),
    );
    expect(withAlias[0]).toMatchObject({
      turnFocusAnchorId: "pending-current",
    });
    expect(hookSource).toContain("turnFocusAnchorAliases.get(item.id)");
    expect(hookSource).toContain("projectZenTimeline(\n      events,");
  });

  test("instrumentation collection is impossible in production builds", () => {
    expect(
      evaluateTimelineProjectionPerfCollectionAllowed({
        dev: false,
        bunPresent: true,
        nodeEnv: "development",
      }),
    ).toBe(false);
    expect(
      evaluateTimelineProjectionPerfCollectionAllowed({
        dev: false,
        bunPresent: false,
        nodeEnv: "production",
      }),
    ).toBe(false);
    expect(
      evaluateTimelineProjectionPerfCollectionAllowed({
        bunPresent: false,
        nodeEnv: "production",
      }),
    ).toBe(false);
    expect(
      evaluateTimelineProjectionPerfCollectionAllowed({
        bunPresent: true,
        nodeEnv: "production",
      }),
    ).toBe(false);
    // Bun/unit tests: __DEV__ absent, non-production NODE_ENV.
    expect(
      evaluateTimelineProjectionPerfCollectionAllowed({
        bunPresent: true,
        nodeEnv: "test",
      }),
    ).toBe(true);
    expect(
      evaluateTimelineProjectionPerfCollectionAllowed({
        bunPresent: true,
      }),
    ).toBe(true);
    expect(
      evaluateTimelineProjectionPerfCollectionAllowed({
        dev: true,
        bunPresent: false,
        nodeEnv: "production",
      }),
    ).toBe(true);

    // Live gate under Bun test must allow, and enable/record must work.
    expect(timelineProjectionPerfCollectionAllowed()).toBe(true);
    enableTimelineProjectionPerf();
    try {
      resetTimelineProjectionPerf();
      projectZenTimeline(makeMixedTimelineEvents(5), null);
      expect(getTimelineProjectionPerfSnapshot().projections.length).toBe(1);
    } finally {
      disableTimelineProjectionPerf();
      resetTimelineProjectionPerf();
    }
  });

  test("streaming revisions reuse cached raw order keys without re-sorting", () => {
    const base = makeMixedTimelineEvents(80);
    const streamId = firstAssistantEventId(base);
    const primed = projectZenTimeline(base, null);
    expect(["already-sorted", "sorted"]).toContain(primed.eventOrder);

    let cache = primed.cache;
    for (let revision = 1; revision <= 5; revision += 1) {
      const next = withAssistantBodyRevision(base, streamId, revision);
      const ordered = resolveProjectionEventOrder(next, cache);
      expect(ordered.source).toBe("cached-ids");
      const projected = projectZenTimeline(next, cache);
      expect(projected.eventOrder).toBe("cached-ids");
      expect(projected.mode).toBe("incremental");
      cache = projected.cache;
    }

    const unsorted = [...cache.sortedEvents].reverse();
    expect(resolveProjectionEventOrder(unsorted, null).source).toBe("sorted");
    expect(resolveProjectionEventOrder(unsorted, cache).source).toBe("sorted");
    // Same event refs after a forced sort are a proven no-op, not a second model.
    const resortedNoop = projectZenTimeline(unsorted, cache);
    expect(resortedNoop.eventOrder).toBe("sorted");
    expect(resortedNoop.mode).toBe("incremental");
    expect(resortedNoop.stableRowChurn).toBe(0);

    const structurallyDifferent = unsorted.map((event, index) =>
      index === 0 ? { ...event, id: `other-${event.id}` } : event,
    );
    const structural = projectZenTimeline(structurallyDifferent, cache);
    expect(structural.eventOrder).toBe("sorted");
    expect(structural.mode).toBe("full");
    expect(structural.fallbackReason).toBe("id-sequence-change");
  });

  test("timestamp/seq crossing rejects cached-ids and matches canonical full", () => {
    const previous: CodexConversationEvent[] = [
      {
        id: "keep-a",
        seq: 1,
        kind: "user_message",
        role: "user",
        body: "first",
        timestamp: "2026-08-06T10:00:00.000Z",
      },
      {
        id: "move-b",
        seq: 2,
        kind: "assistant_message",
        role: "assistant",
        body: "second",
        timestamp: "2026-08-06T11:00:00.000Z",
        partial: true,
      },
    ];
    const primed = projectZenTimeline(previous, null);
    // Same array ID order, but move-b's timestamp/seq now sorts before keep-a.
    const crossed: CodexConversationEvent[] = [
      previous[0],
      {
        ...previous[1],
        timestamp: "2026-08-06T09:00:00.000Z",
        seq: 0,
        body: "second crossed",
      },
    ];
    expect(crossed.map((event) => event.id)).toEqual(["keep-a", "move-b"]);
    expect(resolveProjectionEventOrder(crossed, primed.cache).source).toBe(
      "sorted",
    );
    expect(resolveProjectionEventOrder(crossed, primed.cache).source).not.toBe(
      "cached-ids",
    );

    const projected = projectZenTimeline(crossed, primed.cache);
    expect(projected.eventOrder).toBe("sorted");
    expect(projected.mode).toBe("full");
    expect(projected.cache.sortedEvents.map((event) => event.id)).toEqual([
      "move-b",
      "keep-a",
    ]);
    expect(
      timelineItemsSemanticallyEqual(
        projected.items,
        buildZenTimeline(crossed),
      ),
    ).toBe(true);
  });

  test("duplicate event ids fall back before ambiguous incremental replacement", () => {
    const duplicates: CodexConversationEvent[] = [
      {
        id: "dup",
        seq: 1,
        kind: "assistant_message",
        role: "assistant",
        body: "first copy",
        timestamp: "2026-08-06T12:00:00.000Z",
        partial: true,
      },
      {
        id: "dup",
        seq: 2,
        kind: "assistant_message",
        role: "assistant",
        body: "second copy",
        timestamp: "2026-08-06T12:01:00.000Z",
        partial: true,
      },
    ];
    const primed = projectZenTimeline(duplicates, null);
    expect(primed.mode).toBe("full");
    expect(primed.cache.incrementalSafe).toBe(false);
    expect(primed.cache.itemIndexById).toBeNull();
    expect(primed.items.map((item) => item.id)).toEqual(["dup", "dup"]);

    const next = [
      duplicates[0],
      {
        ...duplicates[1],
        body: "second copy revised",
        partial: true,
      },
    ];
    const projected = projectZenTimeline(next, primed.cache);
    expect(projected.mode).toBe("full");
    expect(projected.fallbackReason).toBe("duplicate-event-id");
    expect(projected.cache.incrementalSafe).toBe(false);
    expect(
      timelineItemsSemanticallyEqual(projected.items, buildZenTimeline(next)),
    ).toBe(true);
    expect(
      (projected.items[1] as { body?: string } | undefined)?.body,
    ).toBe("second copy revised");
  });

  test("perf collector cleanup stays disabled after a failed assertion path", () => {
    enableTimelineProjectionPerf();
    try {
      resetTimelineProjectionPerf();
      expect(isTimelineProjectionPerfEnabled()).toBe(true);
      throw new Error("intentional failure to prove cleanup");
    } catch (error) {
      expect((error as Error).message).toContain("intentional failure");
    } finally {
      disableTimelineProjectionPerf();
      resetTimelineProjectionPerf();
    }
    expect(isTimelineProjectionPerfEnabled()).toBe(false);
    expect(getTimelineProjectionPerfSnapshot().projections).toEqual([]);
  });
});

describe("final architecture ownership contracts", () => {
  test("benchmark precomputes revision arrays outside timed projection loops", async () => {
    const testSource = await Bun.file(
      new URL("./projectZenTimeline.test.ts", import.meta.url),
    ).text();
    const bench = sourceBetween(
      testSource,
      'test("500-event streaming upserts stay under 20% of full projection CPU with stable settled rows"',
      'test("incremental output matches canonical full projection across property cases"',
    );
    expect(bench).toContain("const revisionEvents = Array.from(");
    expect(bench).toContain("withAssistantBodyRevision(base, streamId, revision)");
    expect(bench).toContain("medianRatio");
    expect(bench).toContain('benchmark: "500-event-streaming-projection"');
    // Timed regions must consume precomputed arrays, not rebuild revisions.
    const timedFull = sourceBetween(
      bench,
      "const fullStart = nowMs();",
      "const fullWallMs = nowMs() - fullStart;",
    );
    const timedIncremental = sourceBetween(
      bench,
      "const incrementalStart = nowMs();",
      "const incrementalWallMs = nowMs() - incrementalStart;",
    );
    expect(timedFull).toContain("revisionEvents[revision]");
    expect(timedFull).not.toContain("withAssistantBodyRevision(");
    expect(timedIncremental).toContain("revisionEvents[revision]");
    expect(timedIncremental).not.toContain("withAssistantBodyRevision(");
  });

  test("incremental inherits cache uniqueness; hot path never allocates duplicate Sets", async () => {
    const projectorSource = await Bun.file(
      new URL("./projectZenTimeline.ts", import.meta.url),
    ).text();
    expect(projectorSource).toContain("incrementalSafe: boolean");
    expect(projectorSource).toContain("function computeIncrementalSafety(");
    expect(projectorSource).toContain(
      "incrementalSafe: previous.incrementalSafe",
    );
    expect(projectorSource).toContain(
      "itemIndexById: previous.itemIndexById",
    );
    expect(projectorSource).toContain("if (!previous.incrementalSafe");
    // Hot-path duplicate helpers must not remain as per-revision Set scanners.
    expect(projectorSource).not.toContain("function hasDuplicateEventIds(");
    expect(projectorSource).not.toContain("function hasDuplicateTimelineItemIds(");

    const base = makeMixedTimelineEvents(20);
    const streamId = firstAssistantEventId(base);
    const primed = projectZenTimeline(base, null);
    expect(primed.cache.incrementalSafe).toBe(true);
    const mapRef = primed.cache.itemIndexById;
    expect(mapRef).not.toBeNull();

    const next = withAssistantBodyRevision(base, streamId, 3);
    const incremental = projectZenTimeline(next, primed.cache);
    expect(incremental.mode).toBe("incremental");
    expect(incremental.cache.incrementalSafe).toBe(true);
    expect(incremental.cache.itemIndexById).toBe(mapRef);
  });

  test("disabled instrumentation skips timing and sample construction", async () => {
    const projectorSource = await Bun.file(
      new URL("./projectZenTimeline.ts", import.meta.url),
    ).text();
    expect(projectorSource).toContain(
      "const measure = isTimelineProjectionPerfEnabled();",
    );
    expect(projectorSource).toContain(
      "const started = measure ? nowMs() : 0;",
    );
    expect(projectorSource).toContain("function finishMeasured(");
    expect(projectorSource).toContain("if (measure) {\n    recordTimelineProjectionSample({");

    disableTimelineProjectionPerf();
    resetTimelineProjectionPerf();
    expect(isTimelineProjectionPerfEnabled()).toBe(false);
    projectZenTimeline(makeMixedTimelineEvents(12), null);
    expect(getTimelineProjectionPerfSnapshot().projections).toEqual([]);
  });

  test("hook separates event projection memo from brain-work action attachment", async () => {
    const hookSource = await Bun.file(
      new URL("./useInterfaceTimelineItems.ts", import.meta.url),
    ).text();
    expect(hookSource).toContain("const projectedTimelineItems = useMemo(() => {");
    expect(hookSource).toContain("projectZenTimeline(\n      events,");
    expect(hookSource).toContain("}, [events]);");
    expect(hookSource).toContain(
      "attachBrainWorkEventActions(\n        projectedTimelineItems,",
    );
    expect(hookSource).toContain(
      "[projectedTimelineItems, onBrainWorkEventActivate, openSessionIds]",
    );
    // Callback identity must not be a dependency of the event projector memo.
    const projectionMemo = sourceBetween(
      hookSource,
      "const projectedTimelineItems = useMemo(() => {",
      "const providerTimelineItems = useMemo(",
    );
    expect(projectionMemo).toContain("}, [events]);");
    expect(projectionMemo).not.toContain("onBrainWorkEventActivate");
    expect(projectionMemo).not.toContain("openSessionIds");
  });

  test("cached-order proof uses exact raw timestamp+seq+id without Date.parse or O(n) churn scan", async () => {
    const projectorSource = await Bun.file(
      new URL("./projectZenTimeline.ts", import.meta.url),
    ).text();
    expect(projectorSource).toContain("function eventsHaveSameCachedOrderKeys(");
    expect(projectorSource).toContain("left.timestamp === right.timestamp");
    expect(projectorSource).toContain(
      "const stableRowReuse = items.length - 1;",
    );
    expect(projectorSource).toContain("const stableRowChurn = 1;");
    expect(projectorSource).not.toContain("conversationEventTimeKey");
    // Cached-order helper must not Date.parse; compareConversationEvents may still parse when sorting.
    const cachedOrder = sourceBetween(
      projectorSource,
      "function eventsHaveSameCachedOrderKeys(",
      "function eventsAreSorted(",
    );
    expect(cachedOrder).toContain("left.timestamp === right.timestamp");
    expect(cachedOrder).not.toMatch(/\bDate\.parse\s*\(/);
    expect(projectorSource).not.toContain(
      "if (items[index] === previous.items[index])",
    );

    const previous: CodexConversationEvent[] = [
      {
        id: "a",
        seq: 1,
        kind: "user_message",
        role: "user",
        body: "hi",
        timestamp: "2026-08-06T10:00:00.000Z",
      },
      {
        id: "b",
        seq: 2,
        kind: "assistant_message",
        role: "assistant",
        body: "yo",
        timestamp: "2026-08-06T11:00:00.000Z",
        partial: true,
      },
    ];
    const primed = projectZenTimeline(previous, null);
    // Same parseable instant, different raw string → reject cached-ids.
    const rawChanged: CodexConversationEvent[] = [
      previous[0],
      {
        ...previous[1],
        timestamp: "2026-08-06T11:00:00Z",
        body: "yo!",
      },
    ];
    expect(Date.parse(previous[1].timestamp!)).toBe(
      Date.parse(rawChanged[1].timestamp!),
    );
    expect(resolveProjectionEventOrder(rawChanged, primed.cache).source).not.toBe(
      "cached-ids",
    );
    const projected = projectZenTimeline(rawChanged, primed.cache);
    expect(
      timelineItemsSemanticallyEqual(
        projected.items,
        buildZenTimeline(rawChanged),
      ),
    ).toBe(true);

    const streaming = projectZenTimeline(
      [
        previous[0],
        { ...previous[1], body: "yo streamed", partial: true },
      ],
      primed.cache,
    );
    expect(streaming.mode).toBe("incremental");
    expect(streaming.stableRowReuse).toBe(streaming.items.length - 1);
    expect(streaming.stableRowChurn).toBe(1);
  });
});

function sourceBetween(source: string, start: string, end: string) {
  const startIndex = source.indexOf(start);
  const endIndex = source.indexOf(end, startIndex);
  expect(startIndex).toBeGreaterThanOrEqual(0);
  expect(endIndex).toBeGreaterThan(startIndex);
  return source.slice(startIndex, endIndex);
}

function nowMs() {
  const perf = (
    globalThis as { performance?: { now(): number } }
  ).performance;
  return perf?.now?.() ?? Date.now();
}

function timelineItemsSemanticallyEqual(
  left: ZenTimelineItem[],
  right: ZenTimelineItem[],
) {
  if (left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (stableItemKey(left[index]) !== stableItemKey(right[index])) {
      return false;
    }
  }
  return true;
}

function stableItemKey(item: ZenTimelineItem) {
  const { onPress: _onPress, onRetryPending: _onRetry, ...rest } = item as {
    onPress?: unknown;
    onRetryPending?: unknown;
  } & ZenTimelineItem;
  return JSON.stringify(rest);
}
