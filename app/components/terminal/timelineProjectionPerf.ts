/**
 * Debug/test-only timeline projection instrumentation.
 * Never stores message bodies. Collection is impossible in production builds.
 *
 * JavaScript requestAnimationFrame gaps recorded here are a JS scheduling
 * proxy only — never native UI FrameTimeline / Core Animation FPS.
 */

export type TimelineProjectionMode = "full" | "incremental";

export type TimelineProjectionFallbackReason =
  | "no-cache"
  | "length-change"
  | "id-sequence-change"
  | "multiple-dirty"
  | "kind-change"
  | "non-message"
  | "timestamp-change"
  | "seq-change"
  | "unbounded-field-delta"
  | "presence-change"
  | "item-missing"
  | "duplicate-event-id"
  | "ambiguous";

export type TimelineProjectionSample = {
  mode: TimelineProjectionMode;
  durationMs: number;
  eventCount: number;
  itemCount: number;
  dirtyStart?: number;
  dirtyEnd?: number;
  fallbackReason?: TimelineProjectionFallbackReason;
  stableRowReuse?: number;
  stableRowChurn?: number;
};

export type MarkdownPrepareSample = {
  durationMs: number;
  streaming: boolean;
  inputChars: number;
  outputChars: number;
};

export type MarkdownParseSample = {
  durationMs: number;
  inputChars: number;
  blockCount: number;
};

/** Canonical timeline data-array and row-reference churn. Counts only. */
export type ListDataIdentitySample = {
  scenarioRevision: number;
  itemCount: number;
  dataIdentityChanged: boolean;
  stableItemReferenceReuse: number;
  changedItemReferenceCount: number;
  addedItemCount: number;
  removedItemCount: number;
};

/**
 * JS requestAnimationFrame inter-callback gap.
 * This is a JS scheduling proxy, not native UI FPS.
 */
export type JsFrameGapSample = {
  gapMs: number;
  scenarioRevision: number;
  /** Always true — documents honesty contract in retained samples. */
  jsSchedulingProxy: true;
};

export type DurationPercentileSummary = {
  count: number;
  min: number;
  p50: number;
  p95: number;
  p99: number;
  max: number;
};

type CollectorState = {
  enabled: boolean;
  projections: TimelineProjectionSample[];
  markdownPrepares: MarkdownPrepareSample[];
  markdownParses: MarkdownParseSample[];
  listDataIdentities: ListDataIdentitySample[];
  jsFrameGaps: JsFrameGapSample[];
};

const state: CollectorState = {
  enabled: false,
  projections: [],
  markdownPrepares: [],
  markdownParses: [],
  listDataIdentities: [],
  jsFrameGaps: [],
};

const MAX_SAMPLES = 10_000;

/** Content-free scenario revision for device correlation; harness updates without UI churn. */
let perfScenarioRevision = 0;

export function setTimelineProjectionPerfScenarioRevision(revision: number) {
  perfScenarioRevision = revision;
}

export function getTimelineProjectionPerfScenarioRevision() {
  return perfScenarioRevision;
}

/**
 * Pure gate used by production/runtime checks and regression tests.
 * - `__DEV__ === false` → never (RN/Expo production)
 * - `__DEV__ === true` → allow
 * - `__DEV__` absent + Bun + non-production NODE_ENV → allow (unit tests)
 * - otherwise → deny (no production collection/retention)
 */
export function evaluateTimelineProjectionPerfCollectionAllowed(input: {
  dev?: boolean;
  bunPresent: boolean;
  nodeEnv?: string;
}): boolean {
  if (input.dev === false) {
    return false;
  }
  if (input.dev === true) {
    return true;
  }
  if (input.bunPresent && input.nodeEnv !== "production") {
    return true;
  }
  return false;
}

export function timelineProjectionPerfCollectionAllowed() {
  return evaluateTimelineProjectionPerfCollectionAllowed({
    dev: typeof __DEV__ !== "undefined" ? __DEV__ : undefined,
    bunPresent: typeof Bun !== "undefined",
    nodeEnv: process.env.NODE_ENV,
  });
}

export function enableTimelineProjectionPerf() {
  if (!timelineProjectionPerfCollectionAllowed()) {
    state.enabled = false;
    return;
  }
  state.enabled = true;
}

export function disableTimelineProjectionPerf() {
  state.enabled = false;
}

export function resetTimelineProjectionPerf() {
  state.projections = [];
  state.markdownPrepares = [];
  state.markdownParses = [];
  state.listDataIdentities = [];
  state.jsFrameGaps = [];
  perfScenarioRevision = 0;
}

export function isTimelineProjectionPerfEnabled() {
  return state.enabled && timelineProjectionPerfCollectionAllowed();
}

export function recordTimelineProjectionSample(sample: TimelineProjectionSample) {
  if (!isTimelineProjectionPerfEnabled()) {
    return;
  }
  if (state.projections.length >= MAX_SAMPLES) {
    state.projections.shift();
  }
  state.projections.push({
    mode: sample.mode,
    durationMs: sample.durationMs,
    eventCount: sample.eventCount,
    itemCount: sample.itemCount,
    dirtyStart: sample.dirtyStart,
    dirtyEnd: sample.dirtyEnd,
    fallbackReason: sample.fallbackReason,
    stableRowReuse: sample.stableRowReuse,
    stableRowChurn: sample.stableRowChurn,
  });
}

export function recordMarkdownPrepareSample(sample: MarkdownPrepareSample) {
  if (!isTimelineProjectionPerfEnabled()) {
    return;
  }
  if (state.markdownPrepares.length >= MAX_SAMPLES) {
    state.markdownPrepares.shift();
  }
  // Lengths only — never retain markdown text.
  state.markdownPrepares.push({
    durationMs: sample.durationMs,
    streaming: sample.streaming,
    inputChars: sample.inputChars,
    outputChars: sample.outputChars,
  });
}

export function recordMarkdownParseSample(sample: MarkdownParseSample) {
  if (!isTimelineProjectionPerfEnabled()) {
    return;
  }
  if (state.markdownParses.length >= MAX_SAMPLES) {
    state.markdownParses.shift();
  }
  // Lengths and counts only — never retain markdown text.
  state.markdownParses.push({
    durationMs: sample.durationMs,
    inputChars: sample.inputChars,
    blockCount: sample.blockCount,
  });
}

export function recordListDataIdentitySample(sample: ListDataIdentitySample) {
  if (!isTimelineProjectionPerfEnabled()) {
    return;
  }
  if (state.listDataIdentities.length >= MAX_SAMPLES) {
    state.listDataIdentities.shift();
  }
  state.listDataIdentities.push({
    scenarioRevision: sample.scenarioRevision,
    itemCount: sample.itemCount,
    dataIdentityChanged: sample.dataIdentityChanged,
    stableItemReferenceReuse: sample.stableItemReferenceReuse,
    changedItemReferenceCount: sample.changedItemReferenceCount,
    addedItemCount: sample.addedItemCount,
    removedItemCount: sample.removedItemCount,
  });
}

type TimelineListIdentityItem = { id: string };

/**
 * Canonical InterfaceTimelineView items-array identity probe.
 * Stores counts/booleans only — never ids, message bodies, paths, or inputs.
 */
export function recordTimelineListDataIdentityProbe(input: {
  previousItems: readonly TimelineListIdentityItem[] | null;
  nextItems: readonly TimelineListIdentityItem[];
  scenarioRevision?: number;
}) {
  if (!isTimelineProjectionPerfEnabled()) {
    return;
  }
  const { previousItems, nextItems } = input;
  const scenarioRevision =
    input.scenarioRevision ?? getTimelineProjectionPerfScenarioRevision();
  const dataIdentityChanged = previousItems !== nextItems;
  let stableItemReferenceReuse = 0;
  let changedItemReferenceCount = 0;
  let addedItemCount = 0;
  let removedItemCount = 0;

  if (!previousItems) {
    addedItemCount = nextItems.length;
  } else {
    const previousById = new Map(
      previousItems.map((item) => [item.id, item] as const),
    );
    for (const item of nextItems) {
      const previous = previousById.get(item.id);
      if (!previous) {
        addedItemCount += 1;
        continue;
      }
      previousById.delete(item.id);
      if (previous === item) {
        stableItemReferenceReuse += 1;
      } else {
        changedItemReferenceCount += 1;
      }
    }
    removedItemCount = previousById.size;
  }

  const sample: ListDataIdentitySample = {
    scenarioRevision,
    itemCount: nextItems.length,
    dataIdentityChanged,
    stableItemReferenceReuse,
    changedItemReferenceCount,
    addedItemCount,
    removedItemCount,
  };
  recordListDataIdentitySample(sample);
  return sample;
}

export function recordJsFrameGapSample(sample: {
  gapMs: number;
  scenarioRevision: number;
}) {
  if (!isTimelineProjectionPerfEnabled()) {
    return;
  }
  if (state.jsFrameGaps.length >= MAX_SAMPLES) {
    state.jsFrameGaps.shift();
  }
  state.jsFrameGaps.push({
    gapMs: sample.gapMs,
    scenarioRevision: sample.scenarioRevision,
    jsSchedulingProxy: true,
  });
}

export function getTimelineProjectionPerfSnapshot() {
  if (!timelineProjectionPerfCollectionAllowed()) {
    return {
      enabled: false,
      projections: [] as TimelineProjectionSample[],
      markdownPrepares: [] as MarkdownPrepareSample[],
      markdownParses: [] as MarkdownParseSample[],
      listDataIdentities: [] as ListDataIdentitySample[],
      jsFrameGaps: [] as JsFrameGapSample[],
    };
  }
  return {
    enabled: state.enabled,
    projections: state.projections.slice(),
    markdownPrepares: state.markdownPrepares.slice(),
    markdownParses: state.markdownParses.slice(),
    listDataIdentities: state.listDataIdentities.slice(),
    jsFrameGaps: state.jsFrameGaps.slice(),
  };
}

export function sumProjectionDurationMs(mode?: TimelineProjectionMode) {
  if (!timelineProjectionPerfCollectionAllowed()) {
    return 0;
  }
  return state.projections.reduce((total, sample) => {
    if (mode && sample.mode !== mode) {
      return total;
    }
    return total + sample.durationMs;
  }, 0);
}

export function countProjectionFallbacks() {
  if (!timelineProjectionPerfCollectionAllowed()) {
    return 0;
  }
  return state.projections.filter((sample) => sample.fallbackReason).length;
}

export function sumMarkdownPrepareDurationMs() {
  if (!timelineProjectionPerfCollectionAllowed()) {
    return 0;
  }
  return state.markdownPrepares.reduce(
    (total, sample) => total + sample.durationMs,
    0,
  );
}

export function sumMarkdownParseDurationMs() {
  if (!timelineProjectionPerfCollectionAllowed()) {
    return 0;
  }
  return state.markdownParses.reduce(
    (total, sample) => total + sample.durationMs,
    0,
  );
}

export function summarizeDurationPercentiles(
  values: readonly number[],
): DurationPercentileSummary {
  if (values.length === 0) {
    return { count: 0, min: 0, p50: 0, p95: 0, p99: 0, max: 0 };
  }
  const sorted = values.slice().sort((a, b) => a - b);
  return {
    count: sorted.length,
    min: sorted[0]!,
    p50: nearestRankPercentile(sorted, 50),
    p95: nearestRankPercentile(sorted, 95),
    p99: nearestRankPercentile(sorted, 99),
    max: sorted[sorted.length - 1]!,
  };
}

function nearestRankPercentile(sortedAscending: readonly number[], p: number) {
  const rank = Math.ceil((p / 100) * sortedAscending.length) - 1;
  return sortedAscending[Math.max(0, Math.min(sortedAscending.length - 1, rank))]!;
}

export const JS_FRAME_GAP_METRIC_LABEL =
  "jsRafGapMs (JS scheduling proxy — not native UI FPS)";

export type TimelineProjectionPerfDeviceSummary = {
  enabled: boolean;
  collectionAllowed: boolean;
  projections: {
    all: DurationPercentileSummary;
    full: DurationPercentileSummary;
    incremental: DurationPercentileSummary;
    fallbackCount: number;
  };
  markdownPrepare: DurationPercentileSummary;
  markdownParse: DurationPercentileSummary;
  listDataIdentity: {
    sampleCount: number;
    dataIdentityChangedCount: number;
    stableItemReferenceReuseTotal: number;
    changedItemReferenceTotal: number;
    addedItemTotal: number;
    removedItemTotal: number;
  };
  jsFrameGaps: DurationPercentileSummary & {
    metricLabel: typeof JS_FRAME_GAP_METRIC_LABEL;
  };
};

export function summarizeTimelineProjectionPerf(): TimelineProjectionPerfDeviceSummary {
  const snapshot = getTimelineProjectionPerfSnapshot();
  const projectionDurations = snapshot.projections.map((s) => s.durationMs);
  const fullDurations = snapshot.projections
    .filter((s) => s.mode === "full")
    .map((s) => s.durationMs);
  const incrementalDurations = snapshot.projections
    .filter((s) => s.mode === "incremental")
    .map((s) => s.durationMs);
  return {
    enabled: snapshot.enabled,
    collectionAllowed: timelineProjectionPerfCollectionAllowed(),
    projections: {
      all: summarizeDurationPercentiles(projectionDurations),
      full: summarizeDurationPercentiles(fullDurations),
      incremental: summarizeDurationPercentiles(incrementalDurations),
      fallbackCount: snapshot.projections.filter((s) => s.fallbackReason).length,
    },
    markdownPrepare: summarizeDurationPercentiles(
      snapshot.markdownPrepares.map((s) => s.durationMs),
    ),
    markdownParse: summarizeDurationPercentiles(
      snapshot.markdownParses.map((s) => s.durationMs),
    ),
    listDataIdentity: {
      sampleCount: snapshot.listDataIdentities.length,
      dataIdentityChangedCount: snapshot.listDataIdentities.filter(
        (s) => s.dataIdentityChanged,
      ).length,
      stableItemReferenceReuseTotal: snapshot.listDataIdentities.reduce(
        (total, sample) => total + sample.stableItemReferenceReuse,
        0,
      ),
      changedItemReferenceTotal: snapshot.listDataIdentities.reduce(
        (total, sample) => total + sample.changedItemReferenceCount,
        0,
      ),
      addedItemTotal: snapshot.listDataIdentities.reduce(
        (total, sample) => total + sample.addedItemCount,
        0,
      ),
      removedItemTotal: snapshot.listDataIdentities.reduce(
        (total, sample) => total + sample.removedItemCount,
        0,
      ),
    },
    jsFrameGaps: {
      ...summarizeDurationPercentiles(
        snapshot.jsFrameGaps.map((s) => s.gapMs),
      ),
      metricLabel: JS_FRAME_GAP_METRIC_LABEL,
    },
  };
}

/** Compact copyable text for Android Studio / dumpsys gfxinfo / Instruments correlation. */
export function formatTimelineProjectionPerfDeviceSummary(input?: {
  scenarioId?: string;
  scenarioRevision?: number;
  nativeFollowSuspended?: boolean;
}): string {
  const summary = summarizeTimelineProjectionPerf();
  const lines = [
    "zen-interface-perf",
    `scenario=${input?.scenarioId ?? "none"}`,
    `revision=${input?.scenarioRevision ?? 0}`,
    `followSuspended=${input?.nativeFollowSuspended === true ? "1" : "0"}`,
    `enabled=${summary.enabled ? "1" : "0"}`,
    `allowed=${summary.collectionAllowed ? "1" : "0"}`,
    formatPercentileLine("projection.ms", summary.projections.all),
    formatPercentileLine("projection.full.ms", summary.projections.full),
    formatPercentileLine(
      "projection.incremental.ms",
      summary.projections.incremental,
    ),
    `projection.fallbackCount=${summary.projections.fallbackCount}`,
    formatPercentileLine("markdown.prepare.ms", summary.markdownPrepare),
    formatPercentileLine("markdown.parse.ms", summary.markdownParse),
    `listData.samples=${summary.listDataIdentity.sampleCount}`,
    `listData.identityChanged=${summary.listDataIdentity.dataIdentityChangedCount}`,
    `listData.stableRefReuse=${summary.listDataIdentity.stableItemReferenceReuseTotal}`,
    `listData.changedRef=${summary.listDataIdentity.changedItemReferenceTotal}`,
    `listData.added=${summary.listDataIdentity.addedItemTotal}`,
    `listData.removed=${summary.listDataIdentity.removedItemTotal}`,
    formatPercentileLine(JS_FRAME_GAP_METRIC_LABEL, summary.jsFrameGaps),
    "note=JS_rAF_gaps_are_proxy_not_native_UI_FPS",
    "correlate=Android_Studio_FrameTimeline|adb_dumpsys_gfxinfo|Xcode_Instruments|Core_Animation",
  ];
  return lines.join("\n");
}

function formatPercentileLine(name: string, stats: DurationPercentileSummary) {
  return `${name}: n=${stats.count} min=${fmt(stats.min)} p50=${fmt(stats.p50)} p95=${fmt(stats.p95)} p99=${fmt(stats.p99)} max=${fmt(stats.max)}`;
}

function fmt(value: number) {
  if (!Number.isFinite(value)) {
    return "nan";
  }
  return value < 10 ? value.toFixed(3) : value.toFixed(2);
}

export function timelineProjectionPerfMaxSamples() {
  return MAX_SAMPLES;
}
