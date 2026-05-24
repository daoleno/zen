import type { ConnectionState } from "../../store/agents";
import type { CodexSlashCommand } from "../../services/websocket";
import { filterSlashCommands } from "./CodexSlashCommands";

const TERMINAL_ROUTE_BAR_HEIGHT = 38;

export interface CodexComposerPresentation {
  commandQuery: string;
  visibleSlashCommands: CodexSlashCommand[];
  showCommandMenu: boolean;
  showCommandList: boolean;
  showComposerActions: boolean;
  composerActionButtonEnabled: boolean;
  showStopButton: boolean;
  showStopIndicator: boolean;
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
  requestRunning: boolean;
  attachmentCount: number;
  sending: boolean;
  startingNewChat: boolean;
  interrupting: boolean;
  canSend: boolean;
  composerFocused: boolean;
  actionMenuPinned: boolean;
  safeAreaTop: number;
  safeAreaBottom: number;
  isAndroid: boolean;
}

export function buildCodexComposerPresentation({
  draft,
  slashCommands,
  connectionState,
  requestRunning,
  attachmentCount,
  sending,
  startingNewChat,
  interrupting,
  canSend,
  composerFocused,
  actionMenuPinned,
  safeAreaTop,
  safeAreaBottom,
  isAndroid,
}: CodexComposerPresentationInput): CodexComposerPresentation {
  const commandQuery = draft.trimStart();
  const slashQueryActive =
    commandQuery.startsWith("/") &&
    !commandQuery.includes(" ") &&
    !commandQuery.includes("\n");
  const normalDraftActive = commandQuery.length > 0 && !slashQueryActive;
  const showCommandMenu =
    connectionState === "connected" &&
    (actionMenuPinned || slashQueryActive);
  const showComposerActions =
    connectionState === "connected" && actionMenuPinned;
  const showCommandList =
    showCommandMenu && (slashQueryActive || !normalDraftActive);
  const composerActionButtonEnabled = connectionState === "connected";
  const showStopIndicator =
    connectionState === "connected" &&
    requestRunning &&
    draft.trim().length === 0 &&
    attachmentCount === 0;
  const showStopButton = showStopIndicator && !sending;

  return {
    commandQuery,
    visibleSlashCommands: filterSlashCommands(
      slashCommands,
      slashQueryActive ? commandQuery : "/",
    ),
    showCommandMenu,
    showCommandList,
    showComposerActions,
    composerActionButtonEnabled,
    showStopButton,
    showStopIndicator,
    sendEnabled: canSend || showStopButton || startingNewChat,
    sendIcon: showStopIndicator ? "square" : "arrow-up",
    sendLabel: startingNewChat
      ? "Starting new chat"
      : showStopButton
        ? "Stop Codex"
        : showStopIndicator && interrupting
          ? "Stopping Codex"
          : showStopIndicator
            ? "Codex working"
            : "Send message",
    placeholder:
      connectionState === "connected" ? "Message Codex" : "Daemon unavailable",
    active: composerFocused || showCommandMenu,
    bottomPadding: Math.max(safeAreaBottom, 8),
    keyboardVerticalOffset: isAndroid
      ? safeAreaTop + TERMINAL_ROUTE_BAR_HEIGHT
      : 0,
  };
}
