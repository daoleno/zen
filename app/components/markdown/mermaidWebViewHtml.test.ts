import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, test } from "bun:test";
import { MERMAID_RUNTIME_SHA256, MERMAID_VERSION } from "./mermaidRuntimeMeta";
import { MERMAID_RUNTIME_SOURCE } from "./mermaidRuntimeSource";
import {
  MERMAID_ENGINE_BOOTSTRAP,
  MERMAID_ENGINE_CSP,
  buildMermaidEngineHtml,
  mermaidEngineDocumentLoadsRemoteScripts,
} from "./mermaidWebViewHtml";

describe("Mermaid offline engine document", () => {
  test("pins mermaid 11.6.0 and the exact min.js bytes", () => {
    const min = readFileSync(
      join(import.meta.dir, "../../../node_modules/mermaid/dist/mermaid.min.js"),
    );
    expect(MERMAID_VERSION).toBe("11.6.0");
    expect(createHash("sha256").update(min).digest("hex")).toBe(
      MERMAID_RUNTIME_SHA256,
    );
    expect(
      createHash("sha256").update(MERMAID_RUNTIME_SOURCE).digest("hex"),
    ).toBe(MERMAID_RUNTIME_SHA256);
  });

  test("inlines mermaid with a strict CSP and no remote scripts", () => {
    const html = buildMermaidEngineHtml();
    expect(html).toContain(MERMAID_ENGINE_CSP);
    expect(html).toContain("connect-src 'none'");
    expect(mermaidEngineDocumentLoadsRemoteScripts(html)).toBe(false);
    expect(html).not.toContain("<script src=");
    expect(MERMAID_ENGINE_BOOTSTRAP).toContain('securityLevel: "strict"');
    expect(MERMAID_ENGINE_BOOTSTRAP).toContain("htmlLabels: false");
    expect(MERMAID_ENGINE_BOOTSTRAP).toContain("window.__zenMermaidRender");
    expect(MERMAID_ENGINE_BOOTSTRAP).not.toContain("eval(");
  });
});
