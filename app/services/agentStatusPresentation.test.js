import { describe, expect, test } from "bun:test";
import {
  agentStatusLabel,
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
});
