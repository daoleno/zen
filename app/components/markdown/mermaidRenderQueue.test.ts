import { afterEach, describe, expect, test } from "bun:test";
import {
  MERMAID_MAX_QUEUED_RENDERS,
} from "./mermaidLimits";
import {
  bindMermaidEngineInject,
  completeMermaidEngineResult,
  failMermaidEngine,
  markMermaidEngineReady,
  mermaidEngineQueueSnapshot,
  requestMermaidEngineRender,
  resetMermaidEngineQueue,
} from "./mermaidRenderQueue";
import type { MermaidRenderTheme } from "./mermaidTheme";

const theme: MermaidRenderTheme = {
  mode: "light",
  background: "transparent",
  foreground: "#111",
  lineColor: "#999",
  primaryColor: "#eee",
  primaryTextColor: "#111",
  primaryBorderColor: "#999",
  secondaryColor: "#ddd",
  tertiaryColor: "#ccc",
  clusterBkg: "#f4f4f4",
  clusterBorder: "#999",
  edgeLabelBackground: "#fff",
  fontSize: "13px",
};

function request(generation: number, onResult: (result: { ok: boolean; error?: string }) => void) {
  return requestMermaidEngineRender({
    source: "flowchart TD\n  A-->B",
    theme,
    generation,
    timeoutMs: 50,
    onResult,
  });
}

afterEach(() => {
  resetMermaidEngineQueue();
});

describe("Mermaid engine render queue", () => {
  test("holds jobs until the engine is ready and injects one at a time", () => {
    const injected: string[] = [];
    bindMermaidEngineInject((script) => {
      injected.push(script);
    });
    const results: string[] = [];
    request(1, (result) => {
      results.push(result.ok ? "ok1" : result.error ?? "fail");
    });
    request(2, (result) => {
      results.push(result.ok ? "ok2" : result.error ?? "fail");
    });
    expect(mermaidEngineQueueSnapshot()).toMatchObject({
      queued: 2,
      inflight: false,
      ready: false,
    });
    expect(injected).toEqual([]);
    markMermaidEngineReady();
    expect(injected).toHaveLength(1);
    expect(injected[0]).toContain('"requestId":"m1"');
    expect(mermaidEngineQueueSnapshot().inflight).toBe(true);
    completeMermaidEngineResult("m1", 1, {
      ok: true,
      svg: "<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>",
      width: 10,
      height: 10,
    });
    expect(results).toEqual(["ok1"]);
    expect(injected).toHaveLength(2);
    expect(injected[1]).toContain('"requestId":"m2"');
  });

  test("rejects overflow instead of growing an unbounded queue", () => {
    bindMermaidEngineInject(() => undefined);
    const outcomes: string[] = [];
    for (let index = 0; index < MERMAID_MAX_QUEUED_RENDERS + 2; index += 1) {
      request(index, (result) => {
        outcomes.push(result.ok ? "ok" : result.error ?? "fail");
      });
    }
    expect(outcomes.filter((item) => item === "busy")).toHaveLength(2);
    expect(mermaidEngineQueueSnapshot().pending).toBe(MERMAID_MAX_QUEUED_RENDERS);
  });

  test("cancelled queued jobs never inject, and engine failure settles waiters", () => {
    const injected: string[] = [];
    bindMermaidEngineInject((script) => {
      injected.push(script);
    });
    markMermaidEngineReady();
    const first: string[] = [];
    const second: string[] = [];
    request(1, (result) => {
      first.push(result.ok ? "ok" : result.error ?? "fail");
    });
    const cancelSecond = request(2, (result) => {
      second.push(result.ok ? "ok" : result.error ?? "fail");
    });
    expect(injected).toHaveLength(1);
    cancelSecond();
    failMermaidEngine("engine");
    expect(first).toEqual(["engine"]);
    expect(second).toEqual([]);
    expect(mermaidEngineQueueSnapshot().pending).toBe(0);
  });
});
