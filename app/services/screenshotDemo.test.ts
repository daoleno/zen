// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  resolveScreenshotDemoState,
  screenshotDemoEnabled,
  screenshotDemoRouteOptedIn,
  shouldUseScreenshotDemoRuntime,
} from "./screenshotDemo";

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
    expect(screenshotDemoEnabled({ dev: true, enabled: undefined })).toBe(false);
    expect(screenshotDemoEnabled({ dev: false, enabled: "1" })).toBe(false);
  });

  test("never lets the environment gate hijack ordinary startup", () => {
    const enabled = screenshotDemoEnabled({ dev: true, enabled: "1" });

    expect(shouldUseScreenshotDemoRuntime({
      demo: undefined,
      enabled,
      rootSegment: undefined,
    })).toBe(false);
    expect(shouldUseScreenshotDemoRuntime({
      demo: "1",
      enabled,
      rootSegment: "(primary)",
    })).toBe(false);
    expect(shouldUseScreenshotDemoRuntime({
      demo: "1",
      enabled,
      rootSegment: "terminal",
    })).toBe(false);
    expect(shouldUseScreenshotDemoRuntime({
      demo: undefined,
      enabled,
      rootSegment: "screenshot-demo",
    })).toBe(false);
    expect(shouldUseScreenshotDemoRuntime({
      demo: "1",
      enabled,
      rootSegment: "screenshot-demo",
    })).toBe(true);
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
    expect(resolveScreenshotDemoState("unknown")).toBe("chat");
    expect(resolveScreenshotDemoState(undefined)).toBe("chat");
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
