import { describe, expect, test } from "bun:test";
import { formatTelegramListTime } from "./telegramPresentation";

describe("formatTelegramListTime", () => {
  test("returns an empty label when the activity timestamp is unavailable", () => {
    expect(formatTelegramListTime()).toBe("");
    expect(formatTelegramListTime(undefined)).toBe("");
    expect(formatTelegramListTime(0)).toBe("");
    expect(formatTelegramListTime(Number.NaN)).toBe("");
  });

  test("formats a same-day timestamp as HH:mm", () => {
    const now = new Date();
    const sameDay = new Date(
      now.getFullYear(),
      now.getMonth(),
      now.getDate(),
      9,
      30,
    ).getTime();
    const label = formatTelegramListTime(sameDay);
    expect(label).toMatch(/^\d{2}:\d{2}$/);
  });

  test("never renders the current device time for invalid input", () => {
    const before = Date.now();
    const label = formatTelegramListTime("not-a-timestamp" as any);
    const after = Date.now();
    expect(label).toBe("");
    expect(formatTelegramListTime(after)).not.toBe("");
    expect(before).toBeLessThanOrEqual(after);
  });
});
