// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  elapsedNowForRender,
  elapsedSecondsSince,
  formatComposerElapsedDuration,
  formatElapsedDuration,
  workingTurnElapsedLabel,
} from "./workingTurnElapsed";

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

  test("the first Stop paint after Send samples current wall time", () => {
    expect(elapsedNowForRender(startedMs + 12_000, startedMs + 192_000, true))
      .toBe(startedMs + 192_000);
    expect(elapsedNowForRender(startedMs + 12_000, startedMs + 192_000, false))
      .toBe(startedMs + 12_000);
  });

  test("Composer elapsed labels stay bounded inside one action slot", () => {
    const labels = [
      formatComposerElapsedDuration(0),
      formatComposerElapsedDuration(59),
      formatComposerElapsedDuration(60),
      formatComposerElapsedDuration(59 * 60 + 59),
      formatComposerElapsedDuration(3600),
      formatComposerElapsedDuration(99 * 3600 + 59 * 60 + 59),
      formatComposerElapsedDuration(100 * 3600),
    ];
    expect(labels).toEqual([
      "0s",
      "59s",
      "1:00",
      "59:59",
      "1h00",
      "99h59",
      "99h+",
    ]);
    expect(Math.max(...labels.map((label) => label.length))).toBe(5);
    expect(formatElapsedDuration(3600)).toBe("1h 00m 00s");
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
