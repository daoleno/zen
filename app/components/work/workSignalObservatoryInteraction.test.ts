import { describe, expect, test } from "bun:test";
import {
  createWorkObservatoryAccessibilityProps,
  resolveWorkObservatoryPullIntent,
  resolveWorkObservatoryMotion,
  shouldRevealWorkObservatory,
  WORK_OBSERVATORY_ACCESSIBILITY_ACTIONS,
} from "./workSignalObservatoryInteraction";

const pull = (overrides: Partial<Parameters<typeof resolveWorkObservatoryPullIntent>[0]> = {}) =>
  resolveWorkObservatoryPullIntent({
    touchCount: 1,
    startX: 120,
    dx: 2,
    dy: 20,
    scrollOffsetY: 0,
    ...overrides,
  });

describe("Work observatory pull interaction", () => {
  test("activates only a top-of-list downward pull away from the drawer edge", () => {
    expect(pull()).toBe("activate");
    expect(pull({ scrollOffsetY: 1 })).toBe("fail");
    expect(pull({ startX: 24 })).toBe("fail");
    expect(pull({ dx: 18, dy: 8 })).toBe("fail");
    expect(pull({ dy: -8 })).toBe("fail");
    expect(pull({ dy: 8 })).toBe("pending");
    expect(pull({ touchCount: 2 })).toBe("fail");
  });

  test("reveals only after the original pull distance", () => {
    expect(shouldRevealWorkObservatory(131.99)).toBe(false);
    expect(shouldRevealWorkObservatory(132)).toBe(true);
    expect(shouldRevealWorkObservatory(Number.NaN)).toBe(false);
  });
});

describe("Work observatory accessible interaction", () => {
  test("opens through the nonvisual custom action and ignores unrelated actions", () => {
    let opens = 0;
    const open = () => {
      opens += 1;
    };

    const accessibility = createWorkObservatoryAccessibilityProps(open);
    expect(accessibility.accessibilityActions).toBe(
      WORK_OBSERVATORY_ACCESSIBILITY_ACTIONS,
    );
    expect(accessibility.accessibilityActions).toEqual([
      { name: "open-work-observatory", label: "Open Work" },
    ]);
    accessibility.onAccessibilityAction({
      nativeEvent: { actionName: "activate" },
    });
    expect(opens).toBe(0);
    accessibility.onAccessibilityAction({
      nativeEvent: { actionName: "open-work-observatory" },
    });
    expect(opens).toBe(1);
  });

  test("removes modal and graph transitions under reduced motion", () => {
    expect(resolveWorkObservatoryMotion(false)).toEqual({
      modalAnimationType: "fade",
      animateGraph: true,
    });
    expect(resolveWorkObservatoryMotion(true)).toEqual({
      modalAnimationType: "none",
      animateGraph: false,
    });
  });
});
