import { describe, expect, test } from "bun:test";
import {
  agentStatusIndicatorIcon,
  agentStatusLabel,
  buildAgentSessionAccessibilityLabel,
  isAgentActivelyRunning,
} from "./agentStatusPresentation.ts";

const NON_RUNNING = ["unknown", "done", "failed", "blocked"];

describe("agentStatusPresentation", () => {
  test("Running label and active flag only for running", () => {
    expect(agentStatusLabel("running")).toBe("Running");
    expect(isAgentActivelyRunning("running")).toBe(true);

    for (const status of NON_RUNNING) {
      expect(agentStatusLabel(status)).not.toBe("Running");
      expect(isAgentActivelyRunning(status)).toBe(false);
    }
  });

  test("unknown maps to Idle label and is not Running", () => {
    expect(isAgentActivelyRunning("unknown")).toBe(false);
    expect(agentStatusLabel("unknown")).toBe("Idle");
  });

  test("done/failed/blocked statuses are not Running", () => {
    expect(agentStatusLabel("done")).toBe("Done");
    expect(agentStatusLabel("failed")).toBe("Failed");
    expect(agentStatusLabel("blocked")).toBe("Blocked");
    expect(isAgentActivelyRunning("done")).toBe(false);
    expect(isAgentActivelyRunning("failed")).toBe(false);
    expect(isAgentActivelyRunning("blocked")).toBe(false);
  });

  test("uses a distinct familiar shape for every non-running state", () => {
    expect(agentStatusIndicatorIcon("running")).toBeNull();
    expect(agentStatusIndicatorIcon("done")).toBe("checkmark-circle");
    expect(agentStatusIndicatorIcon("failed")).toBe("close-circle");
    expect(agentStatusIndicatorIcon("blocked")).toBe("pause-circle");
    expect(agentStatusIndicatorIcon("unknown")).toBe("help-circle-outline");
    expect(new Set(NON_RUNNING.map(agentStatusIndicatorIcon)).size).toBe(
      NON_RUNNING.length,
    );
  });

  test("keeps status, Brain origin, and conditional time in row speech", () => {
    expect(
      buildAgentSessionAccessibilityLabel({
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
      buildAgentSessionAccessibilityLabel({
        title: "Active Session",
        status: "running",
        preview: "Implementing",
        timeLabel: "live",
        brainDelegated: false,
      }),
    ).toBe("Active Session, Running, Implementing");
  });
});
