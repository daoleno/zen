import { afterEach, describe, expect, test } from "bun:test";
import { parseMessageBlocks } from "./InterfaceMessageBodyModel";
import { prepareInterfaceMarkdown } from "./InterfaceNativeMarkdownBodyPrepare";
import {
  createStreamingMarkdownFrameController,
  type AnimationFrameHandle,
  type AnimationFrameScheduler,
} from "./streamingMarkdownFrameController";
import {
  STREAMING_MARKDOWN_FIXTURE_KINDS,
  buildStreamingMarkdownRevisions,
} from "./streamingMarkdownFixtures";
import {
  disableTimelineProjectionPerf,
  enableTimelineProjectionPerf,
  getTimelineProjectionPerfSnapshot,
  resetTimelineProjectionPerf,
  sumMarkdownParseDurationMs,
  sumMarkdownPrepareDurationMs,
} from "./timelineProjectionPerf";

afterEach(() => {
  disableTimelineProjectionPerf();
  resetTimelineProjectionPerf();
});

type ManualScheduler = {
  scheduler: AnimationFrameScheduler;
  flushOne(): boolean;
};

function createManualScheduler(): ManualScheduler {
  type Entry = {
    id: number;
    callback: () => void;
    cancelled: boolean;
  };
  const queue: Entry[] = [];
  let nextId = 1;
  return {
    scheduler: {
      request(callback: () => void): AnimationFrameHandle {
        const entry: Entry = {
          id: nextId,
          callback,
          cancelled: false,
        };
        nextId += 1;
        queue.push(entry);
        return entry.id;
      },
      cancel(handle: AnimationFrameHandle) {
        const id = handle as number;
        for (const entry of queue) {
          if (entry.id === id) {
            entry.cancelled = true;
          }
        }
      },
    },
    flushOne() {
      while (queue.length > 0) {
        const entry = queue.shift()!;
        if (entry.cancelled) {
          continue;
        }
        entry.callback();
        return true;
      }
      return false;
    },
  };
}

function nowMs() {
  const perf = (
    globalThis as { performance?: { now(): number } }
  ).performance;
  return perf?.now?.() ?? Date.now();
}

function runPrepareAndParse(body: string) {
  const prepared = prepareInterfaceMarkdown(body, true);
  return parseMessageBlocks(prepared);
}

describe("streaming Markdown frame coalescing measurement", () => {
  test("prepare and parse instrumentation records durations without bodies", () => {
    enableTimelineProjectionPerf();
    try {
      resetTimelineProjectionPerf();
      const body = buildStreamingMarkdownRevisions("markdownListTable", 1)[0]!;
      runPrepareAndParse(body);
      const snapshot = getTimelineProjectionPerfSnapshot();
      expect(snapshot.markdownPrepares.length).toBe(1);
      expect(snapshot.markdownParses.length).toBe(1);
      expect(snapshot.markdownPrepares[0]?.inputChars).toBeGreaterThan(0);
      expect(snapshot.markdownParses[0]?.inputChars).toBeGreaterThan(0);
      expect(snapshot.markdownParses[0]?.blockCount).toBeGreaterThan(0);
      expect(sumMarkdownPrepareDurationMs()).toBeGreaterThanOrEqual(0);
      expect(sumMarkdownParseDurationMs()).toBeGreaterThanOrEqual(0);
      expect(JSON.stringify(snapshot)).not.toContain("item alpha");
      expect(JSON.stringify(snapshot)).not.toContain(body.slice(0, 32));
    } finally {
      disableTimelineProjectionPerf();
      resetTimelineProjectionPerf();
    }
  });

  test("truthful precomputed-fixture benchmark: coalesced prepare/parse beats per-revision", () => {
    const revisionCount = 100;
    const revisionsPerFrame = 5;
    // Precompute every fixture outside timed regions.
    const fixtureRevisions = Object.fromEntries(
      STREAMING_MARKDOWN_FIXTURE_KINDS.map((kind) => [
        kind,
        buildStreamingMarkdownRevisions(kind, revisionCount),
      ]),
    ) as Record<string, string[]>;

    const reports: Array<Record<string, number | string>> = [];

    for (const kind of STREAMING_MARKDOWN_FIXTURE_KINDS) {
      const revisions = fixtureRevisions[kind]!;

      enableTimelineProjectionPerf();
      resetTimelineProjectionPerf();
      // Shared warmup so cold JIT does not dominate either path.
      for (let index = 0; index < 8; index += 1) {
        runPrepareAndParse(revisions[index]!);
      }
      resetTimelineProjectionPerf();

      const beforeStart = nowMs();
      for (let index = 0; index < revisionCount; index += 1) {
        runPrepareAndParse(revisions[index]!);
      }
      const beforeWallMs = nowMs() - beforeStart;
      const beforeSnapshot = getTimelineProjectionPerfSnapshot();
      const beforePrepareCount = beforeSnapshot.markdownPrepares.length;
      const beforeParseCount = beforeSnapshot.markdownParses.length;
      const beforePrepareCpuMs = sumMarkdownPrepareDurationMs();
      const beforeParseCpuMs = sumMarkdownParseDurationMs();

      resetTimelineProjectionPerf();
      const manual = createManualScheduler();
      const publications: string[] = [];
      let publishStreaming = true;
      const controller = createStreamingMarkdownFrameController({
        scheduler: manual.scheduler,
        onPublish: (value) => {
          publications.push(value);
          if (publishStreaming) {
            runPrepareAndParse(value);
            return;
          }
          // Terminal boundary matches MessageBody: exact body, parse only.
          parseMessageBlocks(value);
        },
      });

      const afterStart = nowMs();
      for (let index = 0; index < revisionCount; index += 1) {
        controller.accept(revisions[index]!, true);
        if ((index + 1) % revisionsPerFrame === 0) {
          manual.flushOne();
        }
      }
      // Synchronous exact terminal flush; cancels any older scheduled frame.
      publishStreaming = false;
      controller.accept(revisions[revisionCount - 1]!, false);
      const afterWallMs = nowMs() - afterStart;
      const afterSnapshot = getTimelineProjectionPerfSnapshot();
      const afterPrepareCount = afterSnapshot.markdownPrepares.length;
      const afterParseCount = afterSnapshot.markdownParses.length;
      const afterPrepareCpuMs = sumMarkdownPrepareDurationMs();
      const afterParseCpuMs = sumMarkdownParseDurationMs();
      controller.dispose();
      disableTimelineProjectionPerf();
      resetTimelineProjectionPerf();

      const expectedFrames = Math.floor(revisionCount / revisionsPerFrame);
      expect(publications).toHaveLength(expectedFrames + 1);
      expect(publications[publications.length - 1]).toBe(
        revisions[revisionCount - 1],
      );
      expect(beforePrepareCount).toBe(revisionCount);
      expect(beforeParseCount).toBe(revisionCount);
      // Frame publications prepare+parse; terminal parse-only (no remend).
      expect(afterPrepareCount).toBe(expectedFrames);
      expect(afterParseCount).toBe(expectedFrames + 1);
      expect(afterPrepareCount).toBeLessThan(beforePrepareCount);
      expect(afterParseCount).toBeLessThan(beforeParseCount);

      reports.push({
        kind,
        revisionCount,
        revisionsPerFrame,
        beforePrepareCount,
        afterPrepareCount,
        beforeParseCount,
        afterParseCount,
        beforeWallMs,
        afterWallMs,
        beforePrepareCpuMs,
        afterPrepareCpuMs,
        beforeParseCpuMs,
        afterParseCpuMs,
        prepareCountRatio: afterPrepareCount / beforePrepareCount,
        parseCountRatio: afterParseCount / beforeParseCount,
      });
    }

    console.log(
      JSON.stringify({
        benchmark: "streaming-markdown-frame-coalescing",
        note: "Bun wall-clock CPU evidence for prepare+parse invocation coalescing; not an on-device FPS claim.",
        reports,
      }),
    );
  });
});
