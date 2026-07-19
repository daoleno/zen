// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { getTerminalCapabilityPresentation } from "./terminalCapabilities";

describe("getTerminalCapabilityPresentation", () => {
  test("advertises the shared Ghostty terminal on Android and iOS", () => {
    expect(getTerminalCapabilityPresentation("android")).toEqual({
      supported: true,
      title: "Terminal available",
      detail: "This build uses the native libghostty VT core.",
      hint: "",
    });
    expect(getTerminalCapabilityPresentation("ios")).toEqual({
      supported: true,
      title: "Terminal available",
      detail: "This build uses the native libghostty VT core.",
      hint: "",
    });
  });

  test("keeps non-mobile platforms unsupported", () => {
    const result = getTerminalCapabilityPresentation("web");
    expect(result.supported).toBe(false);
    expect(result.detail).toContain("Android and iOS");
    expect(result.hint).toContain("Android or iOS");
  });
});
