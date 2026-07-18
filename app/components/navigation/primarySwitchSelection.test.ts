import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  applyPrimarySwitchPressIn,
  applyPrimarySwitchTap,
  reconcilePrimarySwitchPending,
} from "./primarySwitchSelection";

describe("primary switch pending navigation", () => {
  test("brain→list records pending list and repeat list does not duplicate", () => {
    const forward = applyPrimarySwitchTap({
      canonicalRoute: "brain",
      pendingRoute: null,
      target: "list",
    });
    expect(forward).toEqual({
      cancelTrace: false,
      pendingRoute: "list",
      shouldNavigate: true,
    });
    expect(
      applyPrimarySwitchTap({
        canonicalRoute: "brain",
        pendingRoute: "list",
        target: "list",
      }),
    ).toEqual({
      cancelTrace: false,
      pendingRoute: "list",
      shouldNavigate: false,
    });
    expect(
      applyPrimarySwitchPressIn({
        canonicalRoute: "brain",
        pendingRoute: "list",
        target: "list",
      }),
    ).toEqual({ cancelTrace: false, openTrace: false });
  });

  test("reverse brain while canonical is still brain and pending is list overrides", () => {
    expect(
      applyPrimarySwitchTap({
        canonicalRoute: "brain",
        pendingRoute: "list",
        target: "brain",
      }),
    ).toEqual({
      cancelTrace: true,
      pendingRoute: null,
      shouldNavigate: true,
    });
    expect(
      applyPrimarySwitchPressIn({
        canonicalRoute: "brain",
        pendingRoute: "list",
        target: "brain",
      }),
    ).toEqual({ cancelTrace: true, openTrace: false });
  });

  test("idle canonical tap is a no-op and catch-up clears pending", () => {
    expect(
      applyPrimarySwitchTap({
        canonicalRoute: "brain",
        pendingRoute: null,
        target: "brain",
      }),
    ).toEqual({
      cancelTrace: false,
      pendingRoute: null,
      shouldNavigate: false,
    });
    expect(reconcilePrimarySwitchPending("list", "list")).toBeNull();
    expect(reconcilePrimarySwitchPending("brain", "list")).toBe("list");
    expect(reconcilePrimarySwitchPending("brain", null)).toBeNull();
  });

  test("settled reverse from list still navigates without pending", () => {
    expect(
      applyPrimarySwitchTap({
        canonicalRoute: "list",
        pendingRoute: null,
        target: "brain",
      }),
    ).toEqual({
      cancelTrace: false,
      pendingRoute: "brain",
      shouldNavigate: true,
    });
  });
});

describe("primary Header pager position bridge", () => {
  test("tabBar publishes Pager position and Header interpolates it natively", () => {
    const layoutSource = readFileSync(
      join(import.meta.dir, "../../app/(primary)/_layout.tsx"),
      "utf8",
    );
    const switchSource = readFileSync(
      join(import.meta.dir, "PrimaryTopSwitch.tsx"),
      "utf8",
    );
    expect(layoutSource).toContain(
      "<PrimaryPagerPositionBridge position={position} />",
    );
    expect(switchSource).toContain("usePrimaryPagerPosition()");
    expect(switchSource).toContain("position.interpolate({");
    expect(switchSource).not.toContain("react-native-reanimated");
    expect(switchSource).not.toContain("position.addListener");
  });
});
