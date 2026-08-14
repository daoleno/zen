// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { buildZenTimeline } from "../components/terminal/InterfaceTimelineModel";
import {
  resolveScreenshotChatPendingFixture,
  resolveScreenshotDemoState,
  resolveScreenshotProvidersDemo,
  screenshotDemoEnabled,
  screenshotDemoRouteOptedIn,
  shouldUseScreenshotDemoRuntime,
} from "./screenshotDemo";
import {
  SCREENSHOT_ACTIVITY_HEADER_EVENTS,
  SCREENSHOT_BRAIN_EVENTS,
  screenshotChatPendingUserMessages,
} from "./screenshotDemoFixtures";

const layoutSource = readFileSync(
  join(import.meta.dir, "../app/_layout.tsx"),
  "utf8",
);
const demoRouteSource = readFileSync(
  join(import.meta.dir, "../app/screenshot-demo.tsx"),
  "utf8",
);

describe("screenshot demo isolation", () => {
  test("requires both a development bundle and an explicit opt-in", () => {
    expect(screenshotDemoEnabled({ dev: true, enabled: "1" })).toBe(true);
    expect(screenshotDemoEnabled({ dev: true, enabled: undefined })).toBe(
      false,
    );
    expect(screenshotDemoEnabled({ dev: false, enabled: "1" })).toBe(false);
  });

  test("never lets the environment gate hijack ordinary startup", () => {
    const enabled = screenshotDemoEnabled({ dev: true, enabled: "1" });

    expect(
      shouldUseScreenshotDemoRuntime({
        demo: undefined,
        enabled,
        rootSegment: undefined,
      }),
    ).toBe(false);
    expect(
      shouldUseScreenshotDemoRuntime({
        demo: "1",
        enabled,
        rootSegment: "(primary)",
      }),
    ).toBe(false);
    expect(
      shouldUseScreenshotDemoRuntime({
        demo: "1",
        enabled,
        rootSegment: "terminal",
      }),
    ).toBe(false);
    expect(
      shouldUseScreenshotDemoRuntime({
        demo: undefined,
        enabled,
        rootSegment: "screenshot-demo",
      }),
    ).toBe(false);
    expect(
      shouldUseScreenshotDemoRuntime({
        demo: "1",
        enabled,
        rootSegment: "screenshot-demo",
      }),
    ).toBe(true);
  });

  test("requires demo=1 on the screenshot route", () => {
    expect(screenshotDemoRouteOptedIn("1")).toBe(true);
    expect(screenshotDemoRouteOptedIn(["1"])).toBe(true);
    expect(screenshotDemoRouteOptedIn(undefined)).toBe(false);
    expect(screenshotDemoRouteOptedIn("true")).toBe(false);
    expect(screenshotDemoRouteOptedIn(["0", "1"])).toBe(false);
  });

  test("accepts only deterministic fixture states", () => {
    expect(resolveScreenshotDemoState("sessions")).toBe("sessions");
    expect(resolveScreenshotDemoState("brain")).toBe("brain");
    expect(resolveScreenshotDemoState("stats")).toBe("stats");
    expect(resolveScreenshotDemoState("calendar")).toBe("calendar");
    expect(resolveScreenshotDemoState("profile")).toBe("profile");
    expect(resolveScreenshotDemoState("providers")).toBe("providers");
    expect(resolveScreenshotDemoState("unknown")).toBe("chat");
    expect(resolveScreenshotDemoState(undefined)).toBe("chat");
  });

  test("providers demo renders the real Providers surface on fixture catalogs", () => {
    expect(demoRouteSource).toContain('case "providers":');
    expect(demoRouteSource).toContain("ProvidersDemo");
    expect(demoRouteSource).toContain("resolveScreenshotProvidersDemo");
    expect(demoRouteSource).toContain(
      'from "../components/providers/ProvidersPresentation"',
    );
    expect(demoRouteSource).toContain("apiKeyAutoFocus={demo.apiKeyAutoFocus}");
    expect(demoRouteSource).not.toContain('kind: "add"');
  });

  test("providers demo resolver binds deterministic editor states", () => {
    const empty = resolveScreenshotProvidersDemo({
      fixture: "empty",
    });
    expect(empty.catalog.connections).toEqual([]);
    expect(empty.catalog.presets.length).toBeGreaterThan(0);
    expect(empty.editor).toBeNull();
    expect(empty.apiKeyAutoFocus).toBe(false);

    const connected = resolveScreenshotProvidersDemo({});
    expect(connected.catalog.connections.length).toBeGreaterThan(0);
    expect(connected.editor).toBeNull();

    const direct = resolveScreenshotProvidersDemo({ editor: "codex" });
    expect(direct.editor).toEqual({ kind: "create", client: "codex" });

    const custom = resolveScreenshotProvidersDemo({ editor: "custom" });
    expect(custom.editor).toEqual({ kind: "create", client: "codex" });

    const create = resolveScreenshotProvidersDemo({ editor: "create" });
    expect(create.editor).toEqual({ kind: "create", client: "codex" });

    const retry = resolveScreenshotProvidersDemo({ editor: "retry" });
    expect(retry.editor).toMatchObject({ kind: "edit", retry: true });

    const needsKey = resolveScreenshotProvidersDemo({ editor: "needs-key" });
    expect(needsKey.editor).toMatchObject({ kind: "edit" });
    expect(needsKey.editor).not.toMatchObject({ retry: true });

    const keyboard = resolveScreenshotProvidersDemo({ keyboard: "1" });
    expect(keyboard.apiKeyAutoFocus).toBe(true);

    expect(
      resolveScreenshotProvidersDemo({ editor: "codex", fixture: "empty" })
        .editor,
    ).toEqual({ kind: "create", client: "codex" });
    expect(
      resolveScreenshotProvidersDemo({ editor: "does-not-exist" }).editor,
    ).toBeNull();
  });

  test("profile state opts into the Interface device performance harness", () => {
    expect(demoRouteSource).toContain('case "profile":');
    expect(demoRouteSource).toContain("InterfaceDevicePerformanceDemoGate");
    expect(demoRouteSource).toContain(
      'from "../components/terminal/InterfaceDevicePerformanceDemo"',
    );
  });

  test("chat pending fixtures stay opt-in and use real Pending rows", () => {
    expect(resolveScreenshotChatPendingFixture(undefined)).toBe("none");
    expect(resolveScreenshotChatPendingFixture("0")).toBe("none");
    expect(resolveScreenshotChatPendingFixture("1")).toBe("pending");
    expect(resolveScreenshotChatPendingFixture("pending")).toBe("pending");
    expect(resolveScreenshotChatPendingFixture("failed")).toBe("failed");
    expect(resolveScreenshotChatPendingFixture("long")).toBe("long");
    expect(screenshotChatPendingUserMessages("none")).toEqual([]);
    expect(screenshotChatPendingUserMessages("pending")[0]).toMatchObject({
      lifecycle: "pending",
      body: "On my way.",
    });
    expect(screenshotChatPendingUserMessages("failed")[0]).toMatchObject({
      lifecycle: "failed",
      failureMessage: "Provider unavailable",
    });
    expect(
      screenshotChatPendingUserMessages("long")[0]?.body.length,
    ).toBeGreaterThan(80);
  });

  test("activity header fixtures cover actionable targets and results", () => {
    const projected = buildZenTimeline(SCREENSHOT_ACTIVITY_HEADER_EVENTS)
      .filter((item) => item.type === "activity")
      .map((item) =>
        item.type === "activity"
          ? { title: item.title, detail: item.detail ?? null }
          : null,
      );
    expect(projected).toEqual([
      { title: "Run sleep 45", detail: "Succeeded" },
      { title: "Search daemonSocketPath", detail: "Done" },
      {
        title: "Run daemon/brain/timeline_test.go",
        detail: "Succeeded",
      },
    ]);
    const brainTitles = buildZenTimeline(SCREENSHOT_BRAIN_EVENTS)
      .filter((item) => item.type === "activity")
      .map((item) => (item.type === "activity" ? item.title : null));
    expect(brainTitles.some((title) => title?.startsWith("Run "))).toBe(true);
    expect(brainTitles).toContain("Search daemonSocketPath");
  });

  test("bypasses the live connection lifecycle and imports no live data clients", () => {
    expect(layoutSource).toContain("return <ScreenshotDemoRuntime />");
    expect(layoutSource).toContain("return <LiveAppRuntime />");
    expect(layoutSource).toContain("shouldUseScreenshotDemoRuntime({");
    expect(layoutSource).toContain("demo: params.demo");
    expect(layoutSource).toContain("rootSegment: segments[0]");
    expect(layoutSource).not.toContain('pathname: "/screenshot-demo"');
    expect(demoRouteSource).toContain(
      "screenshotDemoRouteOptedIn(params.demo)",
    );
    expect(demoRouteSource).toContain('router.replace("/")');
    expect(demoRouteSource).not.toContain('from "../services/websocket"');
    expect(demoRouteSource).not.toContain('from "../services/storage"');
    expect(demoRouteSource).not.toContain('from "../store/brain"');
    expect(demoRouteSource).not.toContain('from "../store/agents"');
  });
});
