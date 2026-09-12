import { describe, expect, test } from "bun:test";
import { applyOrientationPolicy, orientationPolicyFor } from "./screenOrientation";

describe("remote desktop orientation policy", () => {
  test("frees rotation only on the remote desktop route", () => {
    expect(orientationPolicyFor("/remote-desktop")).toBe("free");
    expect(orientationPolicyFor("/remote-desktop?server=fixture")).toBe("free");
    expect(orientationPolicyFor("/remote-desktop#keyboard")).toBe("free");
    expect(orientationPolicyFor("/")).toBe("portrait");
    expect(orientationPolicyFor("/terminal/abc")).toBe("portrait");
    expect(orientationPolicyFor("/work/1")).toBe("portrait");
    expect(orientationPolicyFor("/settings")).toBe("portrait");
    expect(orientationPolicyFor("/remote-desktop-settings")).toBe("portrait");
    expect(orientationPolicyFor(undefined)).toBe("portrait");
    expect(orientationPolicyFor(null)).toBe("portrait");
    expect(orientationPolicyFor("")).toBe("portrait");
  });

  test("no native orientation module degrades to a no-op", async () => {
    // bun test has no Expo native module; applying the policy must not throw.
    await expect(applyOrientationPolicy("free")).resolves.toBe(false);
    await expect(applyOrientationPolicy("portrait")).resolves.toBe(false);
  });
});
