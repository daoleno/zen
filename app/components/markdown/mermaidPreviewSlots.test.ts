import { afterEach, describe, expect, test } from "bun:test";
import { MERMAID_MAX_INLINE_PREVIEW_WEBVIEWS } from "./mermaidLimits";
import {
  acquireMermaidPreviewSlot,
  mermaidPreviewSlotSnapshot,
  releaseMermaidPreviewSlot,
  resetMermaidPreviewSlots,
} from "./mermaidPreviewSlots";

afterEach(() => {
  resetMermaidPreviewSlots();
});

describe("Mermaid inline preview WebView slots", () => {
  test("caps concurrent inline preview WebViews", () => {
    const held: boolean[] = [];
    for (let index = 0; index < MERMAID_MAX_INLINE_PREVIEW_WEBVIEWS + 2; index += 1) {
      held.push(acquireMermaidPreviewSlot());
    }
    expect(held.filter(Boolean)).toHaveLength(MERMAID_MAX_INLINE_PREVIEW_WEBVIEWS);
    expect(mermaidPreviewSlotSnapshot().used).toBe(MERMAID_MAX_INLINE_PREVIEW_WEBVIEWS);
    releaseMermaidPreviewSlot();
    expect(acquireMermaidPreviewSlot()).toBe(true);
    expect(mermaidPreviewSlotSnapshot().used).toBe(MERMAID_MAX_INLINE_PREVIEW_WEBVIEWS);
  });
});
