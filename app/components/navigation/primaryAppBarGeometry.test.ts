import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { resolvePrimaryAppBarGeometry } from "./primaryAppBarGeometry";

type PrimaryRoute = "brain" | "list";

interface PagerFrame {
  route: PrimaryRoute;
  safeAreaTop: number;
  width: number;
}

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

const shellSource = source("PrimaryDrawerShell.tsx");
const primaryLayoutSource = source("../../app/(primary)/_layout.tsx");
const brainSource = source("../../app/(primary)/index.tsx");
const sessionsSource = source("../../app/(primary)/list.tsx");
const screenshotDemoSource = source("../../app/screenshot-demo.tsx");

function sessionsTopForFrame(frame: PagerFrame): number {
  return resolvePrimaryAppBarGeometry(frame.safeAreaTop).contentInset;
}

describe("primary app-bar geometry", () => {
  test("keeps the app bar in one overlay layout mode across pager routes", () => {
    const direct: PagerFrame[] = [
      { route: "brain", safeAreaTop: 47, width: 390 },
      { route: "list", safeAreaTop: 47, width: 390 },
    ];
    const swipe: PagerFrame[] = [
      { route: "brain", safeAreaTop: 47, width: 390 },
      { route: "brain", safeAreaTop: 47, width: 390 },
      { route: "list", safeAreaTop: 47, width: 390 },
    ];

    expect(resolvePrimaryAppBarGeometry(47)).toEqual({
      appBarHeight: 52,
      contentInset: 99,
      layoutMode: "overlay",
      safeAreaTop: 47,
    });
    expect(
      direct.map(
        (frame) => resolvePrimaryAppBarGeometry(frame.safeAreaTop).layoutMode,
      ),
    ).toEqual(["overlay", "overlay"]);
    expect(
      swipe.map(
        (frame) => resolvePrimaryAppBarGeometry(frame.safeAreaTop).layoutMode,
      ),
    ).toEqual(["overlay", "overlay", "overlay"]);
    expect(shellSource).toContain("styles.appBarOverlay,");
    expect(shellSource).not.toContain("floating: boolean");
    expect(shellSource).not.toContain(
      'floating={activePrimaryRoute === "brain"}',
    );
    expect(shellSource).not.toContain("? styles.appBarOverlay");
    expect(primaryLayoutSource).toContain("animationEnabled: true");
    expect(primaryLayoutSource).toContain("lazy: false");
    expect(primaryLayoutSource).toContain("swipeEnabled: true");
  });

  test("gives direct and swipe transitions identical initial and settled Sessions tops", () => {
    const direct: PagerFrame[] = [
      { route: "brain", safeAreaTop: 47, width: 390 },
      { route: "list", safeAreaTop: 47, width: 390 },
    ];
    const swipe: PagerFrame[] = [
      { route: "brain", safeAreaTop: 47, width: 390 },
      { route: "brain", safeAreaTop: 47, width: 390 },
      { route: "list", safeAreaTop: 47, width: 390 },
    ];

    expect(direct.map(sessionsTopForFrame)).toEqual([99, 99]);
    expect(swipe.map(sessionsTopForFrame)).toEqual([99, 99, 99]);
  });

  test("keeps cold, warm, width, and orientation frame pairs deterministic", () => {
    const coldMount: PagerFrame[] = [
      { route: "list", safeAreaTop: 47, width: 390 },
      { route: "list", safeAreaTop: 47, width: 390 },
    ];
    const warmRevisit: PagerFrame[] = [
      { route: "list", safeAreaTop: 47, width: 390 },
      { route: "brain", safeAreaTop: 47, width: 390 },
      { route: "brain", safeAreaTop: 47, width: 390 },
      { route: "list", safeAreaTop: 47, width: 390 },
    ];
    const landscapeSwipe: PagerFrame[] = [
      { route: "brain", safeAreaTop: 0, width: 844 },
      { route: "brain", safeAreaTop: 0, width: 844 },
      { route: "list", safeAreaTop: 0, width: 844 },
    ];
    const widthChange: PagerFrame[] = [
      { route: "list", safeAreaTop: 24, width: 320 },
      { route: "list", safeAreaTop: 24, width: 1024 },
    ];

    expect(coldMount.map(sessionsTopForFrame)).toEqual([99, 99]);
    expect(warmRevisit.map(sessionsTopForFrame)).toEqual([99, 99, 99, 99]);
    expect(landscapeSwipe.map(sessionsTopForFrame)).toEqual([52, 52, 52]);
    expect(widthChange.map(sessionsTopForFrame)).toEqual([76, 76]);

    expect(shellSource).toContain(
      "const geometry = resolvePrimaryAppBarGeometry(topInset);",
    );
    expect(shellSource).toContain("minHeight: geometry.contentInset");
    expect(brainSource).toContain(
      "resolvePrimaryAppBarGeometry(insets.top).contentInset",
    );
    expect(sessionsSource).toContain(
      "style={[styles.container, { marginTop: topChromeInset }]}",
    );
    expect(screenshotDemoSource).toContain(
      "{ backgroundColor: colors.bgPrimary, marginTop: topChromeInset }",
    );
  });
});
