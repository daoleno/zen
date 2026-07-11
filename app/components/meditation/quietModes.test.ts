// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  __setWindowClipEndOverrideMsForTests,
  __getWindowClipEndOverrideMsForTests,
  QUIET_MODES,
  resolveWindowClipEndMs,
  WINDOW_CLIP_MAX_DURATION_MS,
} from "./quietModes";

describe("quietModes", () => {
  test("exposes exactly three top-level modes in stable order", () => {
    expect(QUIET_MODES.map((mode) => mode.key)).toEqual([
      "meditation",
      "mokugyo",
      "window",
    ]);
    expect(QUIET_MODES).toHaveLength(3);
  });

  test("test-only clip-end override seam stays null in production default", () => {
    expect(__getWindowClipEndOverrideMsForTests()).toBeNull();
    __setWindowClipEndOverrideMsForTests(2500);
    expect(__getWindowClipEndOverrideMsForTests()).toBe(2500);
    expect(resolveWindowClipEndMs(45_000)).toBe(2500);
    __setWindowClipEndOverrideMsForTests(null);
    expect(__getWindowClipEndOverrideMsForTests()).toBeNull();
  });

  test("resolveWindowClipEndMs caps long masters and keeps short natural ends", () => {
    expect(resolveWindowClipEndMs(9_941)).toBe(9_941);
    expect(resolveWindowClipEndMs(45_000)).toBe(WINDOW_CLIP_MAX_DURATION_MS);
    expect(resolveWindowClipEndMs(null)).toBe(WINDOW_CLIP_MAX_DURATION_MS);
  });
});
