import type { BrainExecutorRef } from "../../store/brain";
import { AMP_DELEGATION_LIMITATION, isAmpExecutor } from "../../services/ampAgent";

export type ExecutorTarget = "brain" | "workers";

export type BrainExecutorOption = BrainExecutorRef & { unavailableReason?: string };

// The absent Amp row is display-only. It never enters the daemon catalog or
// store, and every selector rejects unavailable options before a callback.
export function brainExecutorOptions(executors: readonly BrainExecutorRef[]): BrainExecutorOption[] {
  const options: BrainExecutorOption[] = executors.map((executor) => isAmpExecutor(executor)
    ? { ...executor, unavailableReason: AMP_DELEGATION_LIMITATION }
    : executor);
  if (!executors.some(isAmpExecutor)) {
    options.push({ id: "amp", name: "Amp", unavailableReason: AMP_DELEGATION_LIMITATION });
  }
  return options;
}

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
    case "amp":
      return "Amp";
    case "tmux":
      return "tmux";
    default:
      return value?.trim() || "Custom";
  }
}

export function brainAdapterLabel(adapter?: BrainExecutorRef | null): string {
  if (!adapter) {
    return "";
  }
  if (isAmpExecutor(adapter) && adapter.id.trim().toLowerCase() === "amp") return "Amp";
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
  adapter?: BrainExecutorRef | null,
): string {
  if (isAmpExecutor(adapter)) return "amp";
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
  host?: BrainExecutorRef | null,
  delegated?: BrainExecutorRef | null,
): BrainExecutorRef[] {
  const out: BrainExecutorRef[] = [];
  if (host?.id) {
    out.push(host);
  }
  if (delegated?.id && delegated.id !== host?.id) {
    out.push(delegated);
  }
  return out;
}

export function switchExecutorAccessibilityLabel(
  host?: BrainExecutorRef | null,
  delegated?: BrainExecutorRef | null,
): string {
  const brain = brainAdapterLabel(host) || "unavailable";
  const agents = brainAdapterLabel(delegated) || "unavailable";
  return `Switch executor, Brain ${brain}, Agents ${agents}`;
}

export function brainStatusLine({
  adapter,
}: {
  adapter?: BrainExecutorRef | null;
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
