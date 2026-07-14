import { describe, expect, test } from "bun:test";
import { localFieldsFromInstant, resolveLocalDateTime } from "./calendarTime";

describe("calendar local time resolution", () => {
  test("resolves a normal local time independently of the device timezone", () => {
    expect(
      resolveLocalDateTime("2026-07-14", "18:20", "Asia/Shanghai"),
    ).toEqual({ status: "resolved", instant: "2026-07-14T10:20:00.000Z" });
  });

  test("rejects a DST spring-forward gap explicitly", () => {
    const result = resolveLocalDateTime(
      "2026-03-08",
      "02:30",
      "America/New_York",
    );
    expect(result.status).toBe("gap");
    if (result.status !== "gap") throw new Error("expected gap");
    expect(result.message).toContain("does not exist");
  });

  test("returns both instants for a repeated fall-back wall time", () => {
    const result = resolveLocalDateTime(
      "2026-11-01",
      "01:30",
      "America/New_York",
    );
    expect(result.status).toBe("ambiguous");
    if (result.status !== "ambiguous") throw new Error("expected ambiguity");
    expect(result.instants).toEqual([
      "2026-11-01T05:30:00.000Z",
      "2026-11-01T06:30:00.000Z",
    ]);
  });

  test("round-trips instant fields in the selected IANA timezone", () => {
    expect(
      localFieldsFromInstant("2026-07-14T10:20:00Z", "Asia/Shanghai"),
    ).toEqual({ date: "2026-07-14", time: "18:20" });
  });
});
