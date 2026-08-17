import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  createInterfaceDevicePerfRunner,
  INTERFACE_DEVICE_PERF_LAUNCH_HINTS,
  startJsFrameGapSampler,
} from "./interfaceDevicePerformanceHarness";
import {
  createFakeInterfaceDevicePerfClock,
  prepareInterfaceDevicePerfScenario,
} from "./interfaceDevicePerformanceScenarios";
import type { ZenTimelineItem } from "./InterfaceTimelineItemView";
import {
  disableTimelineProjectionPerf,
  enableTimelineProjectionPerf,
  evaluateTimelineProjectionPerfCollectionAllowed,
  formatTimelineProjectionPerfDeviceSummary,
  getTimelineProjectionPerfSnapshot,
  JS_FRAME_GAP_METRIC_LABEL,
  recordJsFrameGapSample,
  recordListDataIdentitySample,
  recordMarkdownParseSample,
  recordMarkdownPrepareSample,
  recordTimelineBlankWindowSample,
  recordTimelineListDataIdentityProbe,
  recordTimelineProjectionSample,
  recordTimelineRenderProjectionSample,
  resetTimelineProjectionPerf,
  summarizeDurationPercentiles,
  summarizeTimelineProjectionPerf,
  timelineProjectionPerfMaxSamples,
} from "./timelineProjectionPerf";

const timelineViewSource = readFileSync(
  join(import.meta.dir, "InterfaceTimelineView.tsx"),
  "utf8",
);
const demoSource = readFileSync(
  join(import.meta.dir, "InterfaceDevicePerformanceDemo.tsx"),
  "utf8",
);

function messageItem(id: string): ZenTimelineItem {
  return {
    id,
    type: "message",
    role: "assistant",
    body: `body-for-${id}-SECRET`,
    timestamp: "2026-08-06T12:00:00.000Z",
  } as ZenTimelineItem;
}

describe("interfaceDevicePerformanceHarness collector extensions", () => {
  test("canonical InterfaceTimelineView probes list identity only behind perf gate", () => {
    expect(timelineViewSource).toContain("isTimelineProjectionPerfEnabled()");
    expect(timelineViewSource).toContain("recordTimelineListDataIdentityProbe");
    expect(timelineViewSource).toContain(
      "React.useRef<ZenTimelineItem[] | null>(null)",
    );
  });

  test("profile gate enables collection before mounting and result panel stays collapsed", () => {
    expect(demoSource).toContain(
      "export function InterfaceDevicePerformanceDemoGate()",
    );
    expect(demoSource).toContain("enableTimelineProjectionPerf();");
    expect(demoSource).toContain("readyScenarioId !== scenarioId");
    expect(demoSource).toContain("<InterfaceDevicePerformanceDemo");
    expect(demoSource.indexOf("enableTimelineProjectionPerf();")).toBeLessThan(
      demoSource.indexOf("<InterfaceDevicePerformanceDemo"),
    );
    expect(demoSource).toContain("const [summaryOpen, setSummaryOpen]");
    expect(demoSource).toContain("{summaryOpen ? (");
    expect(demoSource).not.toContain("publishSummaryRef");
  });

  test("disabled collector records nothing and resets clear all lanes", () => {
    disableTimelineProjectionPerf();
    resetTimelineProjectionPerf();
    recordTimelineProjectionSample({
      mode: "full",
      durationMs: 1,
      eventCount: 1,
      itemCount: 1,
    });
    recordListDataIdentitySample({
      scenarioRevision: 1,
      itemCount: 1,
      dataIdentityChanged: true,
      stableItemReferenceReuse: 0,
      changedItemReferenceCount: 0,
      addedItemCount: 1,
      removedItemCount: 0,
    });
    recordJsFrameGapSample({ gapMs: 20, scenarioRevision: 1 });
    expect(getTimelineProjectionPerfSnapshot()).toMatchObject({
      enabled: false,
      projections: [],
      listDataIdentities: [],
      jsFrameGaps: [],
    });

    enableTimelineProjectionPerf();
    try {
      resetTimelineProjectionPerf();
      recordMarkdownPrepareSample({
        durationMs: 2,
        streaming: true,
        inputChars: 10,
        outputChars: 9,
      });
      recordMarkdownParseSample({
        durationMs: 3,
        inputChars: 9,
        blockCount: 1,
      });
      recordTimelineRenderProjectionSample({
        mode: "update",
        durationMs: 0.5,
        sourceItemCount: 10,
        renderItemCount: 11,
        changedSourceCount: 1,
        stableRenderReuse: 10,
        stableRenderChurn: 1,
      });
      recordTimelineBlankWindowSample({
        durationMs: 24,
        scenarioRevision: 2,
        itemCount: 10,
      });
      expect(getTimelineProjectionPerfSnapshot().markdownPrepares.length).toBe(1);
      expect(getTimelineProjectionPerfSnapshot().renderProjections).toHaveLength(1);
      expect(getTimelineProjectionPerfSnapshot().blankWindows).toHaveLength(1);
      resetTimelineProjectionPerf();
      expect(getTimelineProjectionPerfSnapshot()).toMatchObject({
        projections: [],
        markdownPrepares: [],
        markdownParses: [],
        listDataIdentities: [],
        jsFrameGaps: [],
        renderProjections: [],
        blankWindows: [],
      });
    } finally {
      disableTimelineProjectionPerf();
      resetTimelineProjectionPerf();
    }
  });

  test("production gate denies collection even if enable is attempted", () => {
    expect(
      evaluateTimelineProjectionPerfCollectionAllowed({
        dev: false,
        bunPresent: true,
        nodeEnv: "test",
      }),
    ).toBe(false);
  });

  test("bounded retention drops oldest samples across new lanes", () => {
    enableTimelineProjectionPerf();
    try {
      resetTimelineProjectionPerf();
      const max = timelineProjectionPerfMaxSamples();
      for (let index = 0; index < max + 5; index += 1) {
        recordJsFrameGapSample({ gapMs: index, scenarioRevision: index });
        recordListDataIdentitySample({
          scenarioRevision: index,
          itemCount: 1,
          dataIdentityChanged: index % 2 === 0,
          stableItemReferenceReuse: 0,
          changedItemReferenceCount: 1,
          addedItemCount: 0,
          removedItemCount: 0,
        });
      }
      const snapshot = getTimelineProjectionPerfSnapshot();
      expect(snapshot.jsFrameGaps.length).toBe(max);
      expect(snapshot.listDataIdentities.length).toBe(max);
      expect(snapshot.jsFrameGaps[0]?.gapMs).toBe(5);
      expect(snapshot.listDataIdentities[0]?.scenarioRevision).toBe(5);
    } finally {
      disableTimelineProjectionPerf();
      resetTimelineProjectionPerf();
    }
  });

  test("percentiles include count and robust ranks", () => {
    const stats = summarizeDurationPercentiles([
      1, 2, 3, 4, 5, 6, 7, 8, 9, 100,
    ]);
    expect(stats.count).toBe(10);
    expect(stats.min).toBe(1);
    expect(stats.p50).toBe(5);
    expect(stats.p95).toBe(100);
    expect(stats.p99).toBe(100);
    expect(stats.max).toBe(100);
    expect(summarizeDurationPercentiles([]).count).toBe(0);
  });

  test("device summary is content-free and labels JS rAF as proxy", () => {
    enableTimelineProjectionPerf();
    try {
      resetTimelineProjectionPerf();
      recordTimelineProjectionSample({
        mode: "incremental",
        durationMs: 4,
        eventCount: 50,
        itemCount: 50,
        fallbackReason: undefined,
        stableRowReuse: 49,
        stableRowChurn: 1,
      });
      recordTimelineProjectionSample({
        mode: "full",
        durationMs: 12,
        eventCount: 50,
        itemCount: 50,
        fallbackReason: "no-cache",
      });
      recordJsFrameGapSample({ gapMs: 18, scenarioRevision: 2 });
      recordTimelineRenderProjectionSample({
        mode: "update",
        durationMs: 0.4,
        sourceItemCount: 50,
        renderItemCount: 51,
        changedSourceCount: 1,
        stableRenderReuse: 50,
        stableRenderChurn: 1,
      });
      recordTimelineBlankWindowSample({
        durationMs: 21,
        scenarioRevision: 2,
        itemCount: 50,
      });
      recordTimelineListDataIdentityProbe({
        previousItems: [messageItem("a"), messageItem("b")],
        nextItems: [messageItem("a"), messageItem("c")],
        scenarioRevision: 2,
      });

      const summary = summarizeTimelineProjectionPerf();
      expect(summary.projections.fallbackCount).toBe(1);
      expect(summary.jsFrameGaps.metricLabel).toBe(JS_FRAME_GAP_METRIC_LABEL);
      expect(summary.renderProjections.incremental.p50).toBe(0.4);
      expect(summary.blankWindows.count).toBe(1);
      expect(summary.jsFrameGaps.metricLabel).toContain("not native UI FPS");

      const text = formatTimelineProjectionPerfDeviceSummary({
        scenarioId: "500-mixed",
        scenarioRevision: 2,
        nativeFollowSuspended: true,
      });
      expect(text).toContain("scenario=500-mixed");
      expect(text).toContain("followSuspended=1");
      expect(text).toContain(JS_FRAME_GAP_METRIC_LABEL);
      expect(text).toContain("renderProjection.incremental.ms");
      expect(text).toContain("blankWindow.count=1");
      expect(text).toContain("note=JS_rAF_gaps_are_proxy_not_native_UI_FPS");
      expect(text).not.toContain("SECRET");
      expect(text).not.toContain("body-for");

      const snapshotJson = JSON.stringify(getTimelineProjectionPerfSnapshot());
      expect(snapshotJson).not.toContain("SECRET");
      expect(snapshotJson).not.toContain("body-for");
    } finally {
      disableTimelineProjectionPerf();
      resetTimelineProjectionPerf();
    }
  });

  test("canonical list observes post-mount blank windows only in the opt-in collector", () => {
    expect(timelineViewSource).toContain("onViewableItemsChanged={handleViewableItemsChanged}");
    expect(timelineViewSource).toContain("perfSawVisibleRowsRef.current");
    expect(timelineViewSource).toContain("recordTimelineBlankWindowSample({");
    expect(timelineViewSource).toContain("perfItemCountRef.current > 0");
  });

  test("list identity probe records reuse and churn without bodies", () => {
    enableTimelineProjectionPerf();
    try {
      resetTimelineProjectionPerf();
      const a = messageItem("LEAKED-PROVIDER-ID-A");
      const b = messageItem("LEAKED-PROVIDER-ID-B");
      const sameA = a;
      const nextB = messageItem("LEAKED-PROVIDER-ID-B");
      const sample = recordTimelineListDataIdentityProbe({
        previousItems: [a, b],
        nextItems: [sameA, nextB],
        scenarioRevision: 3,
      });
      if (!sample) {
        throw new Error("expected enabled list identity sample");
      }
      expect(sample.dataIdentityChanged).toBe(true);
      expect(sample.stableItemReferenceReuse).toBe(1);
      expect(sample.changedItemReferenceCount).toBe(1);
      expect(sample.addedItemCount).toBe(0);
      expect(sample.removedItemCount).toBe(0);
      expect(JSON.stringify(sample)).not.toContain("SECRET");
      expect(JSON.stringify(getTimelineProjectionPerfSnapshot())).not.toContain(
        "LEAKED-PROVIDER-ID",
      );
    } finally {
      disableTimelineProjectionPerf();
      resetTimelineProjectionPerf();
    }
  });

  test("JS frame gap sampler records proxy gaps only while enabled", () => {
    enableTimelineProjectionPerf();
    try {
      resetTimelineProjectionPerf();
      const callbacks: Array<(time: number) => void> = [];
      let handle = 0;
      const sampler = startJsFrameGapSampler({
        now: () => 1000,
        requestAnimationFrame(callback) {
          callbacks.push(callback);
          handle += 1;
          return handle;
        },
        cancelAnimationFrame() {
          callbacks.length = 0;
        },
        scenarioRevision: () => 7,
      });
      expect(callbacks.length).toBe(1);
      callbacks[0]!(16);
      expect(getTimelineProjectionPerfSnapshot().jsFrameGaps).toEqual([]);
      const second = callbacks[callbacks.length - 1]!;
      second(40);
      expect(getTimelineProjectionPerfSnapshot().jsFrameGaps).toEqual([
        { gapMs: 24, scenarioRevision: 7, jsSchedulingProxy: true },
      ]);
      sampler.stop();
    } finally {
      disableTimelineProjectionPerf();
      resetTimelineProjectionPerf();
    }
  });

  test("overdue runner publishes exactly one atomic revision per scheduled callback", () => {
    const prepared = prepareInterfaceDevicePerfScenario("detached-append");
    const fake = createFakeInterfaceDevicePerfClock(0);
    const revisions: number[] = [];
    let scheduledCallback: (() => void) | undefined;
    let pendingDelay = 0;
    const runner = createInterfaceDevicePerfRunner({
      prepared,
      clock: fake.clock,
      host: {
        publish(state) {
          revisions.push(state.revision);
        },
      },
      schedule(callback, delayMs) {
        scheduledCallback = callback;
        pendingDelay = delayMs;
        return {
          cancel() {
            scheduledCallback = undefined;
          },
        };
      },
    });
    runner.start();
    expect(revisions).toEqual([]);
    expect(pendingDelay).toBe(32);
    expect(typeof scheduledCallback).toBe("function");
    fake.set(10_000);
    const first = scheduledCallback;
    if (!first) {
      throw new Error("expected first scheduled callback");
    }
    first();
    expect(revisions).toEqual([1]);
    expect(pendingDelay).toBe(0);
    const second = scheduledCallback;
    if (!second) {
      throw new Error("expected second scheduled callback");
    }
    second();
    expect(revisions).toEqual([1, 2]);
    runner.stop();
    expect(INTERFACE_DEVICE_PERF_LAUNCH_HINTS).toContain(
      "JS rAF gaps are not native FPS",
    );
  });
});
