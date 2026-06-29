import type { BrainAdapterRef } from "../../store/brain";

export function brainProviderLabel(value?: string): string {
  const normalized = value?.trim().toLowerCase();
  switch (normalized) {
    case "codex":
      return "Codex";
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
  if (normalized === "codex" || normalized === "claude" || normalized === "tmux") {
    return normalized;
  }
  return "custom";
}

export function compactWorkspaceLabel(value?: string): string {
  const trimmed = value?.trim().replace(/\/+$/, "") || "";
  if (!trimmed || trimmed === "/") {
    return "";
  }
  const parts = trimmed.split("/").filter(Boolean);
  if (parts.length === 0) {
    return trimmed;
  }
  if (parts.length <= 2) {
    return parts.join("/");
  }
  return parts.slice(-2).join("/");
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