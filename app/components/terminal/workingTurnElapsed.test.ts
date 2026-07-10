// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  elapsedSecondsSince,
  formatElapsedDuration,
  resolveWorkingTurnStartedAt,
  workingTurnElapsedLabel,
} from "./workingTurnElapsed";

describe("resolveWorkingTurnStartedAt", () => {
  test("uses latest echoed user_message as stable turn start", () => {
    expect(
      resolveWorkingTurnStartedAt({
        events: [
          { kind: "user_message", timestamp: "2026-07-10T10:00:00.000Z" },
          { kind: "command", timestamp: "2026-07-10T10:00:05.000Z" },
          { kind: "user_message", timestamp: "2026-07-10T10:01:00.000Z" },
          { kind: "assistant_message", timestamp: "2026-07-10T10:01:30.000Z" },
        ],
      }),
    ).toBe("2026-07-10T10:01:00.000Z");
  });

  test("ignores later queued pending when an echoed turn exists", () => {
    expect(
      resolveWorkingTurnStartedAt({
        events: [
          { kind: "user_message", timestamp: "2026-07-10T10:00:00.000Z" },
        ],
        pendingUserMessages: [
          { createdAt: "2026-07-10T10:05:00.000Z" },
          { createdAt: "2026-07-10T10:06:00.000Z" },
        ],
      }),
    ).toBe("2026-07-10T10:00:00.000Z");
  });

  test("falls back to earliest pending when nothing is echoed yet", () => {
    expect(
      resolveWorkingTurnStartedAt({
        events: [{ kind: "command", timestamp: "2026-07-10T10:00:00.000Z" }],
        pendingUserMessages: [
          { createdAt: "2026-07-10T10:06:00.000Z" },
          { createdAt: "2026-07-10T10:05:00.000Z" },
        ],
      }),
    ).toBe("2026-07-10T10:05:00.000Z");
  });

  test("returns undefined without user_message or pending", () => {
    expect(
      resolveWorkingTurnStartedAt({
        events: [{ kind: "assistant_message", timestamp: "2026-07-10T10:00:00.000Z" }],
      }),
    ).toBeUndefined();
  });
});

describe("elapsed derivation and recovery", () => {
  const startedAt = "2026-07-10T10:00:00.000Z";
  const startedMs = Date.parse(startedAt);

  test("elapsed is wall-clock delta from startedAt", () => {
    expect(elapsedSecondsSince(startedAt, startedMs + 65_000)).toBe(65);
    expect(formatElapsedDuration(65)).toBe("1m 05s");
    expect(
      workingTurnElapsedLabel({
        startedAt,
        nowMs: startedMs + 65_000,
        active: true,
      }),
    ).toBe("1m 05s");
  });

  test("remount/AppState recovery uses current now against same startedAt", () => {
    const beforeLeave = workingTurnElapsedLabel({
      startedAt,
      nowMs: startedMs + 12_000,
      active: true,
    });
    expect(beforeLeave).toBe("12s");

    const afterReturn = workingTurnElapsedLabel({
      startedAt,
      nowMs: startedMs + 192_000,
      active: true,
    });
    expect(afterReturn).toBe("3m 12s");
  });

  test("inactive or finished yields empty label", () => {
    expect(
      workingTurnElapsedLabel({
        startedAt,
        nowMs: startedMs + 90_000,
        active: false,
      }),
    ).toBe("");
    expect(
      workingTurnElapsedLabel({
        startedAt: undefined,
        nowMs: startedMs + 90_000,
        active: true,
      }),
    ).toBe("");
  });

  test("invalid startedAt does not invent elapsed", () => {
    expect(elapsedSecondsSince("not-a-date", Date.now())).toBeNull();
    expect(
      workingTurnElapsedLabel({
        startedAt: "not-a-date",
        nowMs: Date.now(),
        active: true,
      }),
    ).toBe("");
  });
});
