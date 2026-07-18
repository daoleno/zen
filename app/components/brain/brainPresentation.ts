import type { BrainAdapterRef } from "../../store/brain";

export type ExecutorTarget = "brain" | "agents";

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

export function brainAdapterProviderKey(
  adapter?: BrainAdapterRef | null,
): string {
  const normalized = adapter?.provider?.trim().toLowerCase();
  if (
    normalized === "codex" ||
    normalized === "cursor" ||
    normalized === "grok" ||
    normalized === "claude" ||
    normalized === "tmux"
  ) {
    return normalized;
  }
  return "custom";
}

/** Current Brain Host and Agents executors for trailing Action icons; same id is not duplicated. */
export function distinctExecutorAdapters(
  host?: BrainAdapterRef | null,
  delegated?: BrainAdapterRef | null,
): BrainAdapterRef[] {
  const out: BrainAdapterRef[] = [];
  if (host?.id) {
    out.push(host);
  }
  if (delegated?.id && delegated.id !== host?.id) {
    out.push(delegated);
  }
  return out;
}

export function switchExecutorAccessibilityLabel(
  host?: BrainAdapterRef | null,
  delegated?: BrainAdapterRef | null,
): string {
  const brain = brainAdapterLabel(host) || "unavailable";
  const agents = brainAdapterLabel(delegated) || "unavailable";
  return `Switch executor, Brain ${brain}, Agents ${agents}`;
}

export function brainStatusLine({
  adapter,
}: {
  adapter?: BrainAdapterRef | null;
}): string {
  // Header stays compact — workspace paths live in the workspace viewer, not chrome.
  return brainAdapterLabel(adapter) || "Waiting for connection";
}

export function brainWorkspaceMarkdownPath(path: string): boolean {
  return /\.(md|markdown)$/i.test(path);
}

export function brainWorkspaceEntryAccessibilityLabel(
  kind: string,
  name: string,
): string {
  return `${kind === "directory" ? "Open folder" : "Open file"} ${name}`;
}

export function brainWorkspaceEntryIconName(
  kind: string,
  path: string,
): "folder-outline" | "document-text-outline" | "document-outline" {
  if (kind === "directory") {
    return "folder-outline";
  }
  return brainWorkspaceMarkdownPath(path)
    ? "document-text-outline"
    : "document-outline";
}
