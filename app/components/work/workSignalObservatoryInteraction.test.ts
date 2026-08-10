import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  resolveWorkObservatoryPullIntent,
  shouldRevealWorkObservatory,
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

describe("Work observatory product boundary", () => {
  const listSource = readFileSync(
    join(import.meta.dir, "../../app/(primary)/list.tsx"),
    "utf8",
  );
  const observatorySource = readFileSync(
    join(import.meta.dir, "WorkSignalObservatory.tsx"),
    "utf8",
  );

  test("keeps the observatory out of the Session List layout", () => {
    expect(listSource).toContain("<GestureDetector gesture={workObservatoryPullGesture}>");
    expect(listSource).toContain("<AnimatedSectionList");
    expect(listSource).toContain("<WorkSignalPullPreview");
    expect(listSource).toContain("<WorkSignalObservatory");
    expect(listSource).not.toContain("ListHeaderComponent");
  });

  test("virtualizes dozens of Work rows without an accessible wrapper swallowing them", () => {
    expect(observatorySource).toContain("<FlatList");
    expect(observatorySource).toContain("initialNumToRender={10}");
    expect(observatorySource).toContain("maintainVisibleContentPosition");
    expect(observatorySource).not.toContain("accessible accessibilityLabel={`Work");
  });
});
