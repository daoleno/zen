import { MERMAID_MAX_INLINE_PREVIEW_WEBVIEWS } from "./mermaidLimits";

let used = 0;

export function mermaidPreviewSlotSnapshot() {
  return { used, max: MERMAID_MAX_INLINE_PREVIEW_WEBVIEWS };
}

export function resetMermaidPreviewSlots() {
  used = 0;
}

export function acquireMermaidPreviewSlot() {
  if (used >= MERMAID_MAX_INLINE_PREVIEW_WEBVIEWS) {
    return false;
  }
  used += 1;
  return true;
}

export function releaseMermaidPreviewSlot() {
  if (used > 0) {
    used -= 1;
  }
}
