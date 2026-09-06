import { describe, expect, test } from "bun:test";
import {
  workerStatusIndicatorIcon,
  workerStatusLabel,
  buildWorkerSessionAccessibilityLabel,
  isWorkerActivelyRunning,
} from "./workerStatusPresentation.ts";

const NON_RUNNING = ["unknown", "done", "failed", "blocked"];

describe("workerStatusPresentation", () => {
  test("Running label and active flag only for running", () => {
    expect(workerStatusLabel("running")).toBe("Running");
    expect(isWorkerActivelyRunning("running")).toBe(true);

    for (const status of NON_RUNNING) {
      expect(workerStatusLabel(status)).not.toBe("Running");
      expect(isWorkerActivelyRunning(status)).toBe(false);
    }
  });

  test("unknown maps to Idle label and is not Running", () => {
    expect(isWorkerActivelyRunning("unknown")).toBe(false);
    expect(workerStatusLabel("unknown")).toBe("Idle");
  });

  test("done/failed/blocked statuses are not Running", () => {
    expect(workerStatusLabel("done")).toBe("Done");
    expect(workerStatusLabel("failed")).toBe("Failed");
    expect(workerStatusLabel("blocked")).toBe("Blocked");
    expect(isWorkerActivelyRunning("done")).toBe(false);
    expect(isWorkerActivelyRunning("failed")).toBe(false);
    expect(isWorkerActivelyRunning("blocked")).toBe(false);
  });

  test("uses a distinct familiar shape for every non-running state", () => {
    expect(workerStatusIndicatorIcon("running")).toBeNull();
    expect(workerStatusIndicatorIcon("done")).toBe("checkmark-circle");
    expect(workerStatusIndicatorIcon("failed")).toBe("close-circle");
    expect(workerStatusIndicatorIcon("blocked")).toBe("pause-circle");
    expect(workerStatusIndicatorIcon("unknown")).toBe("help-circle-outline");
    expect(new Set(NON_RUNNING.map(workerStatusIndicatorIcon)).size).toBe(
      NON_RUNNING.length,
    );
  });

  test("keeps status, Brain origin, and conditional time in row speech", () => {
    expect(
      buildWorkerSessionAccessibilityLabel({
        title: "Exact Session title",
        status: "blocked",
        preview: "Waiting for input",
        timeLabel: "2m",
        brainDelegated: true,
      }),
    ).toBe(
      "Exact Session title, Brain delegated, Blocked, Waiting for input, 2m",
    );

    expect(
      buildWorkerSessionAccessibilityLabel({
        title: "Active Session",
        status: "running",
        preview: "Implementing",
        timeLabel: "live",
        brainDelegated: false,
      }),
    ).toBe("Active Session, Running, Implementing");
  });
});
