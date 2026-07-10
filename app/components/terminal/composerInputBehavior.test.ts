// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  COMPOSER_SUBMIT_BEHAVIOR,
  composerReturnKeyType,
} from "./composerInputBehavior";

describe("composer input behavior", () => {
  test("Enter remains a newline action", () => {
    expect(COMPOSER_SUBMIT_BEHAVIOR).toBe("newline");
    expect(composerReturnKeyType("android")).toBe("none");
    expect(composerReturnKeyType("ios")).toBe("default");
    expect(composerReturnKeyType("web")).toBe("default");
  });
});
