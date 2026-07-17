export const TERMINAL_SCROLL_BATCH_INTERVAL_MS = 16;
export const TERMINAL_SCROLL_MAX_BATCH_LINES = 12;
export const TERMINAL_SCROLL_INERTIA_MAX_FRAMES = 24;

export const TERMINAL_SCROLL_CANCEL_REASONS = [
  'new-touch',
  'input',
  'selection',
  'route-blur',
  'disconnect',
  'session-change',
  'jump-live',
  'touch-cancel',
] as const;

export type TerminalScrollCancelReason =
  (typeof TERMINAL_SCROLL_CANCEL_REASONS)[number];

export interface TerminalScrollFrame {
  lines: number;
  keepAnimating: boolean;
}

export interface TerminalScrollGestureController {
  start(x: number, y: number, timestamp: number, blocked: boolean): void;
  move(
    x: number,
    y: number,
    timestamp: number,
    cellHeight: number,
  ): boolean;
  end(timestamp: number): boolean;
  frame(timestamp: number): TerminalScrollFrame;
  cancel(reason: TerminalScrollCancelReason): void;
}

/**
 * Canonical, self-contained JavaScript expression embedded directly into the
 * WebView document. Tests execute this exact source. Production never asks
 * Hermes to serialize a function or reconstruct closure state.
 */
export const TERMINAL_SCROLL_GESTURE_CONTROLLER_SOURCE = `(() => {
  const batchIntervalMs = ${TERMINAL_SCROLL_BATCH_INTERVAL_MS};
  const maxBatchLines = ${TERMINAL_SCROLL_MAX_BATCH_LINES};
  const maxInertiaFrames = ${TERMINAL_SCROLL_INERTIA_MAX_FRAMES};
  const verticalStartThreshold = 4;
  const verticalLockRatio = 1.15;
  const velocityWindowMs = 80;
  const minimumInertiaVelocity = 0.08;
  const stopInertiaVelocity = 0.02;
  const maximumInertiaVelocity = 2.5;
  const inertiaDecay = 0.82;

  let tracking = false;
  let claimed = false;
  let startX = 0;
  let startY = 0;
  let lastY = 0;
  let currentCellHeight = 1;
  let pendingPixels = 0;
  let nextFrameAt = 0;
  let inertiaVelocity = 0;
  let inertiaFrames = 0;
  let lastInertiaAt = 0;
  let samples = [];

  const reset = () => {
    tracking = false;
    claimed = false;
    startX = 0;
    startY = 0;
    lastY = 0;
    currentCellHeight = 1;
    pendingPixels = 0;
    nextFrameAt = 0;
    inertiaVelocity = 0;
    inertiaFrames = 0;
    lastInertiaAt = 0;
    samples = [];
  };

  const hasPendingAnimation = () => {
    return tracking || inertiaVelocity !== 0 ||
      Math.abs(pendingPixels) >= currentCellHeight;
  };

  const addSample = (y, timestamp) => {
    const safeTimestamp = Number.isFinite(timestamp) ? timestamp : 0;
    samples.push({ y, timestamp: safeTimestamp });
    const cutoff = safeTimestamp - velocityWindowMs;
    while (samples.length > 2 && samples[1].timestamp < cutoff) {
      samples.shift();
    }
  };

  return {
    start(x, y, timestamp, blocked) {
      reset();
      if (blocked || !Number.isFinite(x) || !Number.isFinite(y)) {
        return;
      }

      tracking = true;
      startX = x;
      startY = y;
      lastY = y;
      const safeTimestamp = Number.isFinite(timestamp) ? timestamp : 0;
      nextFrameAt = safeTimestamp + batchIntervalMs;
      addSample(y, safeTimestamp);
    },

    move(x, y, timestamp, cellHeight) {
      if (
        !tracking ||
        !Number.isFinite(x) ||
        !Number.isFinite(y) ||
        !Number.isFinite(cellHeight) ||
        cellHeight <= 0
      ) {
        return false;
      }

      if (!claimed) {
        const horizontal = Math.abs(x - startX);
        const vertical = Math.abs(y - startY);
        if (
          vertical <= verticalStartThreshold ||
          vertical <= horizontal * verticalLockRatio
        ) {
          return false;
        }
        claimed = true;
      }

      currentCellHeight = Math.max(1, cellHeight);
      pendingPixels += lastY - y;
      lastY = y;
      addSample(y, timestamp);
      return true;
    },

    end(timestamp) {
      if (!tracking || !claimed) {
        reset();
        return false;
      }

      tracking = false;
      const last = samples[samples.length - 1];
      let first = samples[0];
      const cutoff =
        (Number.isFinite(timestamp) ? timestamp : last.timestamp) - velocityWindowMs;
      for (const sample of samples) {
        if (sample.timestamp >= cutoff) {
          first = sample;
          break;
        }
      }
      const elapsed = last.timestamp - first.timestamp;
      const velocity = elapsed > 0 ? (first.y - last.y) / elapsed : 0;
      if (Math.abs(velocity) >= minimumInertiaVelocity) {
        inertiaVelocity = Math.max(
          -maximumInertiaVelocity,
          Math.min(maximumInertiaVelocity, velocity),
        );
        lastInertiaAt = Number.isFinite(timestamp) ? timestamp : last.timestamp;
      } else {
        inertiaVelocity = 0;
      }
      inertiaFrames = 0;
      samples = [];
      return true;
    },

    frame(timestamp) {
      if (!hasPendingAnimation()) {
        return { lines: 0, keepAnimating: false };
      }

      const safeTimestamp = Number.isFinite(timestamp) ? timestamp : nextFrameAt;
      if (safeTimestamp < nextFrameAt) {
        return { lines: 0, keepAnimating: true };
      }
      nextFrameAt = safeTimestamp + batchIntervalMs;

      if (!tracking && inertiaVelocity !== 0) {
        const elapsed = Math.max(
          0,
          Math.min(batchIntervalMs * 2, safeTimestamp - lastInertiaAt),
        );
        lastInertiaAt = safeTimestamp;
        pendingPixels += inertiaVelocity * elapsed;
        inertiaFrames += 1;
        inertiaVelocity *= inertiaDecay;
        if (
          inertiaFrames >= maxInertiaFrames ||
          Math.abs(inertiaVelocity) < stopInertiaVelocity
        ) {
          inertiaVelocity = 0;
        }
      }

      const completeLines = Math.trunc(pendingPixels / currentCellHeight);
      const lines = completeLines === 0
        ? 0
        : Math.max(-maxBatchLines, Math.min(maxBatchLines, completeLines));
      pendingPixels -= lines * currentCellHeight;

      if (
        !tracking &&
        inertiaVelocity === 0 &&
        Math.abs(pendingPixels) < currentCellHeight
      ) {
        pendingPixels = 0;
      }

      return {
        lines,
        keepAnimating: hasPendingAnimation(),
      };
    },

    cancel(_reason) {
      reset();
    },
  };
})()`;
