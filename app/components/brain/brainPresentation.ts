import type { BrainAdapterRef } from "../../store/brain";
import { compactPathLabel } from "../../services/pathDisplay";

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

export function compactWorkspaceLabel(value?: string): string {
  return compactPathLabel(value, { tailSegments: 2, showFullUpTo: 2 });
}

export function brainStatusLine({
  adapter,
  workspace,
}: {
  adapter?: BrainAdapterRef | null;
  workspace?: string;
}): string {
  const engine = brainAdapterLabel(adapter);
  const cwd = compactWorkspaceLabel(workspace);
  if (engine && cwd) {
    return `${engine} · ${cwd}`;
  }
  return engine || cwd || "Waiting for connection";
}
