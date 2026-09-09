import { describe, expect, test } from "bun:test";
import { MERMAID_MESSAGE_VERSION } from "./mermaidLimits";
import {
  isAllowedMermaidEngineUrl,
  parseMermaidHostMessage,
} from "./mermaidMessages";

describe("Mermaid host messages", () => {
  test("accepts a versioned ready and result payload", () => {
    expect(parseMermaidHostMessage(JSON.stringify({ v: MERMAID_MESSAGE_VERSION, type: "ready" }))).toEqual({
      v: 1,
      type: "ready",
    });
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"></svg>`;
    expect(
      parseMermaidHostMessage(
        JSON.stringify({
          v: 1,
          type: "result",
          requestId: "m1",
          generation: 3,
          ok: true,
          svg,
          width: 10,
          height: 10,
        }),
      ),
    ).toMatchObject({ ok: true, generation: 3, requestId: "m1" });
  });

  test("rejects stale, oversized, or foreign payloads", () => {
    expect(parseMermaidHostMessage("{not json")).toBeNull();
    expect(
      parseMermaidHostMessage(
        JSON.stringify({ v: 2, type: "ready" }),
      ),
    ).toBeNull();
    expect(
      parseMermaidHostMessage(
        JSON.stringify({
          v: 1,
          type: "result",
          requestId: "m1",
          generation: 1,
          ok: true,
          svg: "<div>nope</div>",
          width: 10,
          height: 10,
        }),
      ),
    ).toBeNull();
    expect(
      parseMermaidHostMessage(
        JSON.stringify({
          v: 1,
          type: "navigate",
          url: "https://evil.example",
        }),
      ),
    ).toBeNull();
  });

  test("allows only the local mermaid document URL", () => {
    expect(isAllowedMermaidEngineUrl("https://zen.local/mermaid")).toBe(true);
    expect(isAllowedMermaidEngineUrl("https://cdn.jsdelivr.net/npm/mermaid")).toBe(
      false,
    );
    expect(isAllowedMermaidEngineUrl("file:///etc/passwd")).toBe(false);
    expect(isAllowedMermaidEngineUrl("https://example.com")).toBe(false);
  });
});
