import { describe, expect, test } from "bun:test";
import {
  COMPOSER_ACTION_BAND_HEIGHT,
  COMPOSER_COMPACT_CAPSULE_HEIGHT,
  COMPOSER_EXPANDED_CAPSULE_BASE_HEIGHT,
  COMPOSER_MODEL_CHIP_LEFT_INSET,
  COMPOSER_MODEL_CHIP_REVEAL_RANGE,
  COMPOSER_RADIUS_COMPACT,
  COMPOSER_RADIUS_EXPANDED,
  composerActionBandHeight,
  composerActionButtonBottomInset,
  composerExpansionRadius,
  composerExpansionTarget,
  composerInputHorizontalPadding,
  composerModelChipReveal,
  composerMotionDisabled,
} from "./composerExpansionMetrics";

describe("composer expansion metrics", () => {
  test("band height moves linearly from 0 to the action band", () => {
    expect(composerActionBandHeight(0)).toBe(0);
    expect(composerActionBandHeight(0.5)).toBe(COMPOSER_ACTION_BAND_HEIGHT / 2);
    expect(composerActionBandHeight(1)).toBe(COMPOSER_ACTION_BAND_HEIGHT);
    expect(composerActionBandHeight(2)).toBe(COMPOSER_ACTION_BAND_HEIGHT);
    expect(composerActionBandHeight(-1)).toBe(0);
  });

  test("capsule base grows by exactly the action band", () => {
    expect(
      COMPOSER_EXPANDED_CAPSULE_BASE_HEIGHT - COMPOSER_COMPACT_CAPSULE_HEIGHT,
    ).toBe(COMPOSER_ACTION_BAND_HEIGHT);
    expect(COMPOSER_COMPACT_CAPSULE_HEIGHT).toBe(56);
  });

  test("corner radius interpolates between compact and expanded", () => {
    expect(composerExpansionRadius(0)).toBe(COMPOSER_RADIUS_COMPACT);
    expect(composerExpansionRadius(1)).toBe(COMPOSER_RADIUS_EXPANDED);
    expect(composerExpansionRadius(0.5)).toBe(
      (COMPOSER_RADIUS_COMPACT + COMPOSER_RADIUS_EXPANDED) / 2,
    );
  });

  test("buttons stay bottom-anchored for every progress", () => {
    expect(composerActionButtonBottomInset(0)).toBe(6);
    expect(composerActionButtonBottomInset(1)).toBe(6);
  });

  test("input clears the compact action circles then widens when expanded", () => {
    const compact = composerInputHorizontalPadding(0);
    expect(compact.left).toBe(50);
    expect(compact.right).toBe(74);
    const expanded = composerInputHorizontalPadding(1);
    expect(expanded.left).toBe(6);
    expect(expanded.right).toBe(8);
    const half = composerInputHorizontalPadding(0.5);
    expect(half.left).toBe(28);
    expect(half.right).toBe(41);
  });

  test("model control reveal is monotonic and hidden in the compact state", () => {
    expect(composerModelChipReveal(0)).toEqual({
      opacity: 0,
      translateY: 10,
    });
    expect(composerModelChipReveal(1)).toEqual({ opacity: 1, translateY: 0 });
    const low = composerModelChipReveal(COMPOSER_MODEL_CHIP_REVEAL_RANGE[0]);
    const high = composerModelChipReveal(COMPOSER_MODEL_CHIP_REVEAL_RANGE[1]);
    expect(low.opacity).toBe(0);
    expect(high.opacity).toBe(1);
    const mid = composerModelChipReveal(0.55);
    expect(mid.opacity).toBeGreaterThan(0);
    expect(mid.opacity).toBeLessThan(1);
  });

  test("chip sits between the Plus and Send/Stop slots", () => {
    expect(COMPOSER_MODEL_CHIP_LEFT_INSET).toBeGreaterThan(44);
    expect(COMPOSER_MODEL_CHIP_LEFT_INSET).toBeLessThan(74);
  });

  test("expansion target is driven by focus only", () => {
    expect(composerExpansionTarget(false)).toBe(0);
    expect(composerExpansionTarget(true)).toBe(1);
  });

  test("reduced motion disables spatial motion", () => {
    expect(composerMotionDisabled(true)).toBe(true);
    expect(composerMotionDisabled(false)).toBe(false);
    expect(composerMotionDisabled(null)).toBe(false);
    expect(composerMotionDisabled(undefined)).toBe(false);
  });
});
