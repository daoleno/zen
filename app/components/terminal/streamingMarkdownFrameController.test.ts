import { describe, expect, test } from "bun:test";
import {
  createStreamingMarkdownFrameController,
  type AnimationFrameHandle,
  type AnimationFrameScheduler,
} from "./streamingMarkdownFrameController";
import {
  STREAMING_MARKDOWN_FIXTURE_KINDS,
  buildStreamingMarkdownRevisions,
  streamingMarkdownFixtureBody,
} from "./streamingMarkdownFixtures";

type ManualScheduler = {
  scheduler: AnimationFrameScheduler;
  pendingCount(): number;
  flushOne(): boolean;
  /** Invoke the most recently requested callback even if cancelled. */
  invokeLatestRegardlessOfCancel(): void;
};

function createManualScheduler(): ManualScheduler {
  type Entry = {
    id: number;
    callback: () => void;
    cancelled: boolean;
  };
  const queue: Entry[] = [];
  let nextId = 1;
  let latest: Entry | null = null;

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
        latest = entry;
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
    pendingCount() {
      return queue.filter((entry) => !entry.cancelled).length;
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
    invokeLatestRegardlessOfCancel() {
      latest?.callback();
    },
  };
}

describe("streamingMarkdownFixtures", () => {
  test("establishes deterministic plain, list/table, unfinished fence, and large-code bodies", () => {
    expect(STREAMING_MARKDOWN_FIXTURE_KINDS).toEqual([
      "plainText",
      "markdownListTable",
      "unfinishedFence",
      "largeCode",
    ]);
    for (const kind of STREAMING_MARKDOWN_FIXTURE_KINDS) {
      const first = streamingMarkdownFixtureBody(kind, 0);
      const again = streamingMarkdownFixtureBody(kind, 0);
      const next = streamingMarkdownFixtureBody(kind, 1);
      expect(first).toBe(again);
      expect(next.length).toBeGreaterThan(0);
      expect(next).not.toBe(first);
    }
    expect(streamingMarkdownFixtureBody("markdownListTable", 2)).toContain("| Col A |");
    expect(streamingMarkdownFixtureBody("markdownListTable", 2)).toContain("- item alpha");
    expect(streamingMarkdownFixtureBody("unfinishedFence", 3)).toContain("```ts");
    expect(streamingMarkdownFixtureBody("unfinishedFence", 3).trimEnd().endsWith("```")).toBe(
      false,
    );
    expect(streamingMarkdownFixtureBody("largeCode", 4).startsWith("```ts")).toBe(true);
    expect(buildStreamingMarkdownRevisions("plainText", 5)).toHaveLength(5);
  });
});

describe("createStreamingMarkdownFrameController", () => {
  test("burst revisions schedule one frame and the newest revision wins", () => {
    const published: string[] = [];
    const manual = createManualScheduler();
    const controller = createStreamingMarkdownFrameController({
      scheduler: manual.scheduler,
      onPublish: (value) => published.push(value),
    });

    controller.accept("r1", true);
    controller.accept("r2", true);
    controller.accept("r3", true);
    expect(manual.pendingCount()).toBe(1);
    expect(published).toEqual([]);

    expect(manual.flushOne()).toBe(true);
    expect(published).toEqual(["r3"]);
    expect(manual.pendingCount()).toBe(0);
  });

  test("one frame produces at most one publication", () => {
    const published: string[] = [];
    const manual = createManualScheduler();
    const controller = createStreamingMarkdownFrameController({
      scheduler: manual.scheduler,
      onPublish: (value) => published.push(value),
    });

    controller.accept("a", true);
    controller.accept("b", true);
    manual.flushOne();
    expect(published).toEqual(["b"]);
    expect(manual.flushOne()).toBe(false);
    expect(published).toEqual(["b"]);
  });

  test("a subsequent burst schedules exactly one new frame", () => {
    const published: string[] = [];
    const manual = createManualScheduler();
    const controller = createStreamingMarkdownFrameController({
      scheduler: manual.scheduler,
      onPublish: (value) => published.push(value),
    });

    controller.accept("a1", true);
    controller.accept("a2", true);
    manual.flushOne();
    expect(published).toEqual(["a2"]);

    controller.accept("b1", true);
    controller.accept("b2", true);
    controller.accept("b3", true);
    expect(manual.pendingCount()).toBe(1);
    manual.flushOne();
    expect(published).toEqual(["a2", "b3"]);
    expect(manual.pendingCount()).toBe(0);
  });

  test("terminal flush is synchronous and exact", () => {
    const published: string[] = [];
    const manual = createManualScheduler();
    const controller = createStreamingMarkdownFrameController({
      scheduler: manual.scheduler,
      onPublish: (value) => published.push(value),
    });

    controller.accept("partial", true);
    expect(manual.pendingCount()).toBe(1);
    controller.accept("FINAL_BODY", false);
    expect(published).toEqual(["FINAL_BODY"]);
    expect(manual.pendingCount()).toBe(0);
    expect(manual.flushOne()).toBe(false);
  });

  test("a late cancelled callback cannot overwrite terminal text", () => {
    const published: string[] = [];
    const manual = createManualScheduler();
    const controller = createStreamingMarkdownFrameController({
      scheduler: manual.scheduler,
      onPublish: (value) => published.push(value),
    });

    controller.accept("stale-stream", true);
    controller.accept("terminal-exact", false);
    expect(published).toEqual(["terminal-exact"]);
    manual.invokeLatestRegardlessOfCancel();
    expect(published).toEqual(["terminal-exact"]);
  });

  test("dispose/unmount prevents publication", () => {
    const published: string[] = [];
    const manual = createManualScheduler();
    const controller = createStreamingMarkdownFrameController({
      scheduler: manual.scheduler,
      onPublish: (value) => published.push(value),
    });

    controller.accept("pending", true);
    expect(manual.pendingCount()).toBe(1);
    controller.dispose();
    expect(manual.pendingCount()).toBe(0);
    expect(manual.flushOne()).toBe(false);
    manual.invokeLatestRegardlessOfCancel();
    expect(published).toEqual([]);

    controller.accept("after-dispose", true);
    controller.accept("after-dispose-terminal", false);
    expect(published).toEqual([]);
  });

  test("replacement owner cannot inherit another message pending revision", () => {
    const firstPublished: string[] = [];
    const secondPublished: string[] = [];
    const firstManual = createManualScheduler();
    const secondManual = createManualScheduler();
    const first = createStreamingMarkdownFrameController({
      scheduler: firstManual.scheduler,
      onPublish: (value) => firstPublished.push(value),
    });
    const second = createStreamingMarkdownFrameController({
      scheduler: secondManual.scheduler,
      onPublish: (value) => secondPublished.push(value),
    });

    first.accept("message-a-r1", true);
    first.accept("message-a-r2", true);
    first.dispose();
    second.accept("message-b-r1", true);
    secondManual.flushOne();
    firstManual.invokeLatestRegardlessOfCancel();

    expect(firstPublished).toEqual([]);
    expect(secondPublished).toEqual(["message-b-r1"]);
  });
});
