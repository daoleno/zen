import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, test } from "bun:test";

describe("Mermaid WebView Fabric props", () => {
  test("passes dataDetectorTypes as an array so Android Fabric does not abort", () => {
    const host = readFileSync(join(import.meta.dir, "MermaidEngineHost.tsx"), "utf8");
    const preview = readFileSync(join(import.meta.dir, "MermaidSvgPreview.tsx"), "utf8");
    expect(host).not.toContain('dataDetectorTypes="none"');
    expect(preview).not.toContain('dataDetectorTypes="none"');
    expect(host).toContain('dataDetectorTypes={["none"]}');
    expect(preview).toContain('dataDetectorTypes={["none"]}');
  });
});
