import type { BrainAdapterRef } from "../../store/brain";

export function brainProviderLabel(value?: string): string {
  const normalized = value?.trim().toLowerCase();
  switch (normalized) {
    case "codex":
      return "Codex";
    case "cursor":
      return "Cursor Agent";
    case "grok":
      return "Grok";
    case "claude":
      return "Claude Code";
    case "tmux":
      return "tmux";
    default:
      return value?.trim() || "Custom";
  }
}

export function brainAdapterLabel(adapter?: BrainAdapterRef | null): string {
  if (!adapter) {
    return "";
  }
  if (adapter.name?.trim()) {
    return adapter.name.trim();
  }
  return brainProviderLabel(
    adapter.provider && adapter.provider !== "custom"
      ? adapter.provider
      : adapter.id,
  );
}

export function brainAdapterProviderKey(adapter?: BrainAdapterRef | null): string {
  const normalized = adapter?.provider?.trim().toLowerCase();
  if (normalized === "codex" || normalized === "cursor" || normalized === "grok" || normalized === "claude" || normalized === "tmux") {
    return normalized;
  }
  return "custom";
}

export function brainStatusLine({
  adapter,
}: {
  adapter?: BrainAdapterRef | null;
}): string {
  // Header stays compact — workspace paths live in the workspace viewer, not chrome.
  return brainAdapterLabel(adapter) || "Waiting for connection";
}
