// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

describe("Codex native Markdown platform wiring", () => {
  test("Android keeps the conservative fallback with link-capable caller wiring", () => {
    const componentDir = import.meta.dir;
    const androidOverride = join(componentDir, "CodexNativeMarkdownBody.android.tsx");
    const androidRenderer = readFileSync(androidOverride, "utf8");
    const brainWorkspace = readFileSync(
      join(componentDir, "../brain/BrainWorkspaceViewer.tsx"),
      "utf8",
    );

    expect(existsSync(androidOverride)).toBe(true);
    expect(androidRenderer).toContain("renderFallback(markdown || value)");
    expect(androidRenderer).not.toContain("EnrichedMarkdownText");
    expect(brainWorkspace).toContain("<MarkdownFallbackText");
  });
});
