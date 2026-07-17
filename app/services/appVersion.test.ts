import { describe, expect, test } from "bun:test";
import { resolveAppVersion } from "./appVersion";

describe("visible App version", () => {
  test("projects the Expo version without a numeric fallback copy", () => {
    expect(resolveAppVersion(" 0.1.0-beta.5 ")).toBe("0.1.0-beta.5");
    expect(resolveAppVersion(undefined)).toBe("dev");
    expect(resolveAppVersion(" ")).toBe("dev");
  });
});
