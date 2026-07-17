import { describe, expect, test } from "bun:test";
import { TerminalLiveGridOwner } from "./terminalLiveGrid";
import { TerminalSessionCorrelation } from "./terminalSessionCorrelation";
import {
  TERMINAL_RENDERER_MAX_AUTO_RETRIES,
  TERMINAL_RENDERER_READY_TIMEOUT_MS,
  TerminalFontCache,
  createTerminalRendererBootstrapState,
  isCurrentTerminalRendererGeneration,
  reduceTerminalRendererBootstrap,
  terminalRendererPresentation,
} from "./terminalSurfaceBootstrap";

describe("Terminal Surface bounded bootstrap", () => {
  test("Chat to Terminal first mount can fall back from a missing font without permanent loading", async () => {
    const diagnostics: string[] = [];
    const fonts = new TerminalFontCache();
    const font = fonts.resolve(
      () => ({ uri: null, localUri: null }),
      (message) => diagnostics.push(message),
    );

    expect(font.uri).toBeNull();
    expect(font.source).toBe("fallback");
    await font.backgroundLoad;
    expect(diagnostics).toEqual(["Bundled terminal font has no usable URI; using monospace fallback."]);

    let renderer = createTerminalRendererBootstrapState();
    expect(terminalRendererPresentation(renderer)).toBe("loading");
    renderer = reduceTerminalRendererBootstrap(renderer, {
      type: "ready",
      generation: renderer.generation,
    });
    expect(terminalRendererPresentation(renderer)).toBe("ready");
  });

  test("an existing font cache mounts without reloading the asset", async () => {
    const fonts = new TerminalFontCache();
    const first = fonts.resolve(
      () => ({ uri: "file:///terminal.ttf", localUri: "file:///terminal.ttf" }),
      () => {},
    );
    await first.backgroundLoad;

    let loads = 0;
    const cached = fonts.resolve(
      () => {
        loads += 1;
        throw new Error("cached font must not reload");
      },
      () => {},
    );
    await cached.backgroundLoad;

    expect(cached).toMatchObject({
      uri: "file:///terminal.ttf",
      source: "cache",
    });
    expect(loads).toBe(0);
  });

  test("font download rejection is observable but retains the immediate URI", async () => {
    const diagnostics: string[] = [];
    const fonts = new TerminalFontCache();
    const font = fonts.resolve(
      () => ({
        uri: "http://127.0.0.1:8081/terminal.ttf",
        localUri: null,
        downloadAsync: async () => {
          throw new Error("fixture download rejected");
        },
      }),
      (message) => diagnostics.push(message),
    );

    expect(font).toMatchObject({
      uri: "http://127.0.0.1:8081/terminal.ttf",
      source: "asset",
    });
    await font.backgroundLoad;
    expect(diagnostics).toEqual([
      "Bundled terminal font download failed; retaining the immediate URI.",
    ]);
  });

  test("WebView timeout and inline script failure retry once, then expose failure", () => {
    expect(TERMINAL_RENDERER_READY_TIMEOUT_MS).toBeGreaterThan(0);
    expect(TERMINAL_RENDERER_READY_TIMEOUT_MS).toBeLessThanOrEqual(5_000);
    expect(TERMINAL_RENDERER_MAX_AUTO_RETRIES).toBe(1);

    let renderer = createTerminalRendererBootstrapState();
    const firstGeneration = renderer.generation;
    renderer = reduceTerminalRendererBootstrap(renderer, {
      type: "timeout",
      generation: firstGeneration,
      message: "Terminal renderer did not become ready.",
    });
    expect(renderer).toMatchObject({ status: "loading", autoRetries: 1 });
    expect(renderer.generation).toBe(firstGeneration + 1);

    renderer = reduceTerminalRendererBootstrap(renderer, {
      type: "failure",
      generation: renderer.generation,
      message: "fixture inline script failure",
    });
    expect(renderer).toMatchObject({
      status: "failed",
      error: "fixture inline script failure",
    });
    expect(terminalRendererPresentation(renderer)).toBe("failure");

    renderer = reduceTerminalRendererBootstrap(renderer, { type: "retry" });
    expect(renderer).toMatchObject({ status: "loading", autoRetries: 0 });
    renderer = reduceTerminalRendererBootstrap(renderer, {
      type: "ready",
      generation: renderer.generation,
    });
    expect(terminalRendererPresentation(renderer)).toBe("ready");
  });

  test("a renderer process failure after ready gets one bounded recovery", () => {
    let renderer = createTerminalRendererBootstrapState();
    renderer = reduceTerminalRendererBootstrap(renderer, {
      type: "ready",
      generation: renderer.generation,
    });
    const firstGeneration = renderer.generation;

    renderer = reduceTerminalRendererBootstrap(renderer, {
      type: "failure",
      generation: firstGeneration,
      message: "fixture render process exited",
    });
    expect(renderer).toMatchObject({ status: "loading", autoRetries: 1 });
    expect(renderer.generation).toBe(firstGeneration + 1);

    renderer = reduceTerminalRendererBootstrap(renderer, {
      type: "ready",
      generation: renderer.generation,
    });
    renderer = reduceTerminalRendererBootstrap(renderer, {
      type: "failure",
      generation: renderer.generation,
      message: "fixture repeated render process exit",
    });
    expect(renderer).toMatchObject({
      status: "failed",
      error: "fixture repeated render process exit",
    });
  });

  test("Fast Refresh remount ignores stale readiness and starts clean from cache", async () => {
    const fonts = new TerminalFontCache();
    await fonts.resolve(
      () => ({ uri: "file:///cached.ttf", localUri: "file:///cached.ttf" }),
      () => {},
    ).backgroundLoad;

    let renderer = createTerminalRendererBootstrapState();
    const oldGeneration = renderer.generation;
    renderer = reduceTerminalRendererBootstrap(renderer, {
      type: "failure",
      generation: oldGeneration,
      message: "old WebView failed",
    });
    const remountGeneration = renderer.generation;
    expect(isCurrentTerminalRendererGeneration(remountGeneration, remountGeneration)).toBe(true);
    expect(isCurrentTerminalRendererGeneration(remountGeneration, oldGeneration)).toBe(false);
    expect(isCurrentTerminalRendererGeneration(remountGeneration, Number.NaN)).toBe(false);
    renderer = reduceTerminalRendererBootstrap(renderer, {
      type: "load-start",
      generation: oldGeneration,
    });
    renderer = reduceTerminalRendererBootstrap(renderer, {
      type: "ready",
      generation: oldGeneration,
    });
    renderer = reduceTerminalRendererBootstrap(renderer, {
      type: "failure",
      generation: oldGeneration,
      message: "late old WebView failure",
    });

    const remountedFont = fonts.resolve(
      () => {
        throw new Error("remount should use cache");
      },
      () => {},
    );
    expect(renderer).toMatchObject({
      generation: remountGeneration,
      status: "loading",
      autoRetries: 1,
    });
    renderer = reduceTerminalRendererBootstrap(renderer, {
      type: "ready",
      generation: remountGeneration,
    });

    expect(remountedFont.source).toBe("cache");
    expect(terminalRendererPresentation(renderer)).toBe("ready");
  });

  test("normal resize to open to ready happens once and duplicate readiness cannot replay open", () => {
    const events: string[] = [];
    const correlation = new TerminalSessionCorrelation();
    const owner = new TerminalLiveGridOwner({
      requestPtyGrid(cols, rows) {
        events.push(`resize:${cols}x${rows}`);
        if (correlation.beginOpen()) {
          events.push("open");
        }
      },
      resetGhostty() {
        events.push("ghostty-ready");
        return true;
      },
      resizeGhostty() {
        return true;
      },
    });
    let renderer = createTerminalRendererBootstrapState();

    expect(owner.update({ cols: 44, rows: 18, cellWidth: 8, cellHeight: 20 })).toBe(true);
    expect(correlation.acceptOpened("session-a")).toBe(true);
    expect(owner.attach("session-a")).toBe(true);
    renderer = reduceTerminalRendererBootstrap(renderer, {
      type: "ready",
      generation: renderer.generation,
    });
    renderer = reduceTerminalRendererBootstrap(renderer, {
      type: "ready",
      generation: renderer.generation,
    });
    expect(owner.update({ cols: 44, rows: 18, cellWidth: 8, cellHeight: 20 })).toBe(false);

    expect(events).toEqual(["resize:44x18", "open", "ghostty-ready"]);
    expect(terminalRendererPresentation(renderer)).toBe("ready");
  });
});
