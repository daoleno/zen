import type { AgentStatus } from "../../constants/tokens";
import type { ConnectionState } from "../../store/agents";
import type { CodexSlashCommand } from "../../services/websocket";
import { filterSlashCommands } from "./CodexSlashCommands";

const TERMINAL_ROUTE_BAR_HEIGHT = 38;

export interface CodexComposerPresentation {
  commandQuery: string;
  visibleSlashCommands: CodexSlashCommand[];
  showCommandMenu: boolean;
  showStopButton: boolean;
  sendEnabled: boolean;
  sendIcon: "square" | "arrow-up";
  sendLabel: string;
  placeholder: string;
  active: boolean;
  bottomPadding: number;
  keyboardVerticalOffset: number;
}

export interface CodexComposerPresentationInput {
  draft: string;
  slashCommands: CodexSlashCommand[];
  connectionState: ConnectionState;
  agentStatus?: AgentStatus;
  attachmentCount: number;
  sending: boolean;
  canSend: boolean;
  composerFocused: boolean;
  safeAreaTop: number;
  safeAreaBottom: number;
  isAndroid: boolean;
}

export function buildCodexComposerPresentation({
  draft,
  slashCommands,
  connectionState,
  agentStatus,
  attachmentCount,
  sending,
  canSend,
  composerFocused,
  safeAreaTop,
  safeAreaBottom,
  isAndroid,
}: CodexComposerPresentationInput): CodexComposerPresentation {
  const commandQuery = draft.trimStart();
  const showCommandMenu =
    connectionState === "connected" &&
    commandQuery.startsWith("/") &&
    !commandQuery.includes(" ");
  const showStopButton =
    connectionState === "connected" &&
    agentStatus === "running" &&
    draft.trim().length === 0 &&
    attachmentCount === 0 &&
    !sending;

  return {
    commandQuery,
    visibleSlashCommands: filterSlashCommands(slashCommands, commandQuery),
    showCommandMenu,
    showStopButton,
    sendEnabled: canSend || showStopButton,
    sendIcon: showStopButton ? "square" : "arrow-up",
    sendLabel: showStopButton ? "Stop Codex" : "Send message",
    placeholder:
      connectionState === "connected" ? "Message Codex" : "Daemon unavailable",
    active: composerFocused || showCommandMenu,
    bottomPadding: Math.max(safeAreaBottom, 8),
    keyboardVerticalOffset: isAndroid
      ? safeAreaTop + TERMINAL_ROUTE_BAR_HEIGHT
      : 0,
  };
}
