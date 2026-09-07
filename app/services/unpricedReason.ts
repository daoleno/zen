import type { ModelStat } from "./statsPayload";

export function unpricedReasonLabel(reason: ModelStat["unpricedReason"]): string | null {
  switch (reason) {
    case "missing_model": return "Price not in catalog";
    case "missing_rate": return "Some token rates unavailable";
    case "insufficient_context": return "Price found; request context unavailable";
    case "missing_usage": return "Usage details unavailable";
    default: return null;
  }
}
