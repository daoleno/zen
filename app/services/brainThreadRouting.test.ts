import { describe, expect, test } from "bun:test";
import { isTargetedBrainThreadReadOnly } from "./brainThreadRouting";

describe("Brain targeted-thread routing", () => {
  test("a notification-targeted thread is read-only unless it is the live Host thread", () => {
    expect(isTargetedBrainThreadReadOnly("historical", "live")).toBe(true);
    expect(isTargetedBrainThreadReadOnly("historical", undefined)).toBe(true);
    expect(isTargetedBrainThreadReadOnly("live", "live")).toBe(false);
    expect(isTargetedBrainThreadReadOnly(undefined, "live")).toBe(false);
  });
});
