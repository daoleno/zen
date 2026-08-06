/**
 * Device-performance harness helpers for the Interface timeline.
 *
 * Measures existing production owners via the content-free collector.
 * Does not introduce a second list, projection, or Markdown model.
 */

import {
  isTimelineProjectionPerfEnabled,
  recordJsFrameGapSample,
  setTimelineProjectionPerfScenarioRevision,
} from "./timelineProjectionPerf";
import type {
  InterfaceDevicePerfClock,
  InterfaceDevicePerfScenarioState,
  PreparedInterfaceDevicePerfScenario,
} from "./interfaceDevicePerformanceScenarios";
import { createInterfaceDevicePerfScenarioController } from "./interfaceDevicePerformanceScenarios";

export type JsFrameGapSamplerOptions = {
  now(): number;
  requestAnimationFrame(callback: (time: number) => void): number;
  cancelAnimationFrame(handle: number): void;
  scenarioRevision(): number;
};

/**
 * Samples JS requestAnimationFrame inter-callback gaps.
 * Results are a JS scheduling proxy — never native UI FPS.
 */
export function startJsFrameGapSampler(options: JsFrameGapSamplerOptions) {
  let handle: number | null = null;
  let lastTs: number | null = null;
  let stopped = false;

  const tick = (ts: number) => {
    if (stopped) {
      return;
    }
    const now = Number.isFinite(ts) ? ts : options.now();
    if (lastTs !== null && isTimelineProjectionPerfEnabled()) {
      const gapMs = Math.max(0, now - lastTs);
      recordJsFrameGapSample({
        gapMs,
        scenarioRevision: options.scenarioRevision(),
      });
    }
    lastTs = now;
    handle = options.requestAnimationFrame(tick);
  };

  handle = options.requestAnimationFrame(tick);

  return {
    stop() {
      stopped = true;
      if (handle !== null) {
        options.cancelAnimationFrame(handle);
        handle = null;
      }
    },
  };
}

export type InterfaceDevicePerfRunnerHost = {
  /** One atomic scenario snapshot per prepared provider revision. */
  publish(state: InterfaceDevicePerfScenarioState): void;
};

export type InterfaceDevicePerfRunner = {
  start(): void;
  stop(): void;
  /** Manual/test step ignoring schedule delays. */
  stepOnce(): boolean;
  getState(): InterfaceDevicePerfScenarioState;
};

/**
 * Drives a prepared scenario with an injectable clock + schedule.
 * Fake clocks make progression unit-testable without RN timers.
 */
export function createInterfaceDevicePerfRunner(input: {
  prepared: PreparedInterfaceDevicePerfScenario;
  clock: InterfaceDevicePerfClock;
  host: InterfaceDevicePerfRunnerHost;
  schedule(callback: () => void, delayMs: number): { cancel(): void };
}): InterfaceDevicePerfRunner {
  const controller = createInterfaceDevicePerfScenarioController({
    prepared: input.prepared,
    clock: input.clock,
  });
  let scheduled: { cancel(): void } | null = null;
  let stopped = true;

  const publish = () => {
    const state = controller.getState();
    setTimelineProjectionPerfScenarioRevision(state.revision);
    input.host.publish(state);
  };

  const scheduleNext = () => {
    if (stopped || controller.isDone()) {
      return;
    }
    const dueAtMs = controller.nextDueAtMs();
    if (dueAtMs === null) {
      return;
    }
    const delayMs = Math.max(0, dueAtMs - input.clock.now());
    scheduled?.cancel();
    scheduled = input.schedule(() => {
      scheduled = null;
      // One callback owns one provider revision. If the clock is overdue, the
      // next revision is scheduled separately with delay 0 instead of being
      // collapsed into this React update.
      if (!controller.advanceOne()) {
        return;
      }
      publish();
      scheduleNext();
    }, delayMs);
  };

  return {
    start() {
      stopped = false;
      scheduleNext();
    },
    stop() {
      stopped = true;
      scheduled?.cancel();
      scheduled = null;
    },
    stepOnce() {
      const advanced = controller.advanceOne();
      if (advanced) {
        publish();
      }
      return advanced;
    },
    getState() {
      return controller.getState();
    },
  };
}

export const INTERFACE_DEVICE_PERF_LAUNCH_HINTS = [
  "Dev-only. Requires EXPO_PUBLIC_ZEN_SCREENSHOT_DEMO=1 and demo=1.",
  "Android: EXPO_PUBLIC_ZEN_SCREENSHOT_DEMO=1 bun run app:android then open",
  "  zen://screenshot-demo?demo=1&state=profile&scenario=50-short",
  "iOS: EXPO_PUBLIC_ZEN_SCREENSHOT_DEMO=1 bun run app:ios then open the same URL.",
  "Scenarios: 50-short | 500-mixed | stream-8k | detached-append | detached-prepend",
  "Correlate summary with Android Studio FrameTimeline, adb shell dumpsys gfxinfo,",
  "Xcode Instruments Time Profiler, and Core Animation — JS rAF gaps are not native FPS.",
].join("\n");
