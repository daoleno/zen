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
  showActionMenuButton: boolean;
  actionMenuIcon: "add" | "happy-outline";
  showAttachmentRail: boolean;
  composerActionButtonEnabled: boolean;
  showStopButton: boolean;
  showStopIndicator: boolean;
  sendEnabled: boolean;
  sendIcon: "square" | "arrow-up" | "send";
  sendLabel: string;
  sendElapsedLabel?: string;
  placeholder: string;
  bottomPadding: number;
  keyboardVerticalOffset: number;
  automaticKeyboardOffset: boolean;
  minimalComposer: boolean;
  composerLayout: "chatgpt" | "telegram" | "classic";
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
  elapsedLabel?: string;
  actionMenuPinned: boolean;
  safeAreaTop: number;
  safeAreaBottom: number;
  isAndroid: boolean;
  placeholder?: string;
  keyboardVerticalOffset?: number;
  minimalComposer?: boolean;
  showAttachmentControl?: boolean;
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
  elapsedLabel,
  actionMenuPinned,
  safeAreaTop,
  safeAreaBottom,
  isAndroid,
  placeholder,
  keyboardVerticalOffset,
  minimalComposer,
  showAttachmentControl,
}: CodexComposerPresentationInput): CodexComposerPresentation {
  const commandQuery = draft.trimStart();
  const slashQueryActive =
    commandQuery.startsWith("/") &&
    !commandQuery.includes(" ") &&
    !commandQuery.includes("\n");
  const normalDraftActive = commandQuery.length > 0 && !slashQueryActive;
  const composerActionsAvailable =
    !minimalComposer || Boolean(showAttachmentControl);
  const showComposerActions =
    connectionState === "connected" &&
    actionMenuPinned &&
    composerActionsAvailable;
  const showCommandList =
    !minimalComposer &&
    connectionState === "connected" &&
    (actionMenuPinned || slashQueryActive) &&
    (slashQueryActive || !normalDraftActive);
  const showCommandMenu = showComposerActions || showCommandList;
  const composerActionButtonEnabled =
    connectionState === "connected" && composerActionsAvailable;
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
    showActionMenuButton: composerActionsAvailable,
    actionMenuIcon: "add",
    showAttachmentRail: composerActionsAvailable,
    composerActionButtonEnabled,
    showStopButton,
    showStopIndicator,
    sendEnabled: canSend || showStopButton || startingNewChat,
    sendIcon: showStopIndicator ? "square" : minimalComposer ? "arrow-up" : "arrow-up",
    sendLabel: startingNewChat
      ? "Starting new chat"
      : showStopButton
        ? "Stop response"
        : showStopIndicator && interrupting
          ? "Stopping"
          : showStopIndicator
            ? "Working"
            : "Send message",
    sendElapsedLabel: showStopIndicator ? elapsedLabel : undefined,
    placeholder:
      placeholder ||
      (connectionState === "connected" ? "Message Codex" : "Connection unavailable"),
    bottomPadding: Math.max(safeAreaBottom, 8),
    keyboardVerticalOffset:
      typeof keyboardVerticalOffset === "number"
        ? keyboardVerticalOffset
        : isAndroid
          ? safeAreaTop + TERMINAL_ROUTE_BAR_HEIGHT
          : 0,
    automaticKeyboardOffset: typeof keyboardVerticalOffset === "number",
    minimalComposer: Boolean(minimalComposer),
    composerLayout: minimalComposer ? "chatgpt" : "classic",
  };
}
