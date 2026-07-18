import { useCallback, type RefObject } from "react";
import type { LayoutChangeEvent } from "react-native";
import type { useTerminalGitDiff } from "../../components/terminal/useTerminalGitDiff";
import type { TerminalSurfaceHandle } from "../../components/terminal/TerminalSurface";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { Agent, ConnectionState } from "../../store/agents";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type {
  StoredCodexRenderMode,
} from "../../services/storage";
import type { PresentedAgent } from "../../services/agentPresentation";
import type { SessionResourceSnapshot } from "../../services/sessionResourceSnapshot";
import type { useTerminalScreenChrome } from "./useTerminalScreenChrome";
import type { useTerminalSessionActions } from "./useTerminalSessionActions";
import type { useTerminalNavigationActions } from "./useTerminalNavigationActions";
import type { useTerminalViewportModel } from "./useTerminalViewportModel";
import { useTerminalScreenOverlayProps } from "./useTerminalScreenOverlayProps";
import { useTerminalTopBarProps } from "./useTerminalTopBarProps";
import { useTerminalViewportProps } from "./useTerminalViewportProps";

interface UseTerminalScreenLayoutPropsInput {
  agent?: Agent;
  agentId: string;
  accessoryBottomOffset: number;
  chrome: TerminalThemeChrome;
  chatChrome: TerminalThemeChrome;
  chatTheme: TerminalThemePalette;
  chromeLayout: ReturnType<typeof useTerminalScreenChrome>;
  codexRenderMode: StoredCodexRenderMode;
  connectionIssue?: ConnectionIssue | null;
  connectionState: ConnectionState;
  creatingSession: boolean;
  ctrlArmed: boolean;
  daemonId?: string;
  displayName: string;
  gitDiff: ReturnType<typeof useTerminalGitDiff>;
  handleAccessoryLayout(event: LayoutChangeEvent): void;
  handleCtrlArmedChange(next: boolean): void;
  hasLinkedWork: boolean;
  hasTerminalRoute: boolean;
  isStructuredChatAgent: boolean;
  keyboardVisible: boolean;
  menuPosition: { left: number; top: number };
  menuVisible: boolean;
  newTerminalVisible: boolean;
  outputBottomInset: number;
  presentedAgent: PresentedAgent;
  renameDraft: string;
  renamePlaceholder: string;
  renameVisible: boolean;
  resourceSheetVisible: boolean;
  resourceSheetLoading: boolean;
  resourceSheetError?: string | null;
  resourceSheetSnapshot?: SessionResourceSnapshot | null;
  screenFocused: boolean;
  selectedServerId: string;
  serverId: string;
  serverUrl?: string;
  sessionKey: string | null;
  setNewTerminalVisible(value: boolean): void;
  setRenameDraft(value: string): void;
  setRenameVisible(value: boolean): void;
  showCodexChat: boolean;
  navigationActions: ReturnType<typeof useTerminalNavigationActions>;
  terminalRef: RefObject<TerminalSurfaceHandle | null>;
  terminalTheme: TerminalThemePalette;
  viewportModel: ReturnType<typeof useTerminalViewportModel>;
  openGitDiff(): void;
  openNewTerminal(): void;
  openRenameModal(): void;
  openSessionDetails(): void;
  closeResourceSheet(): void;
  retryResourceSheet(): void;
  sessionActions: ReturnType<typeof useTerminalSessionActions>;
}

export function useTerminalScreenLayoutProps({
  agent,
  agentId,
  accessoryBottomOffset,
  chrome,
  chatChrome,
  chatTheme,
  chromeLayout,
  codexRenderMode,
  connectionIssue,
  connectionState,
  creatingSession,
  ctrlArmed,
  daemonId,
  displayName,
  gitDiff,
  handleAccessoryLayout,
  handleCtrlArmedChange,
  hasLinkedWork,
  hasTerminalRoute,
  isStructuredChatAgent,
  keyboardVisible,
  menuPosition,
  menuVisible,
  newTerminalVisible,
  outputBottomInset,
  presentedAgent,
  renameDraft,
  renamePlaceholder,
  renameVisible,
  resourceSheetVisible,
  resourceSheetLoading,
  resourceSheetError,
  resourceSheetSnapshot,
  screenFocused,
  selectedServerId,
  serverId,
  serverUrl,
  sessionKey,
  setNewTerminalVisible,
  setRenameDraft,
  setRenameVisible,
  showCodexChat,
  navigationActions,
  terminalRef,
  terminalTheme,
  viewportModel,
  openGitDiff,
  openNewTerminal,
  openRenameModal,
  openSessionDetails,
  closeResourceSheet,
  retryResourceSheet,
  sessionActions,
}: UseTerminalScreenLayoutPropsInput) {
  const handleToggleCodexRenderMode = useCallback(() => {
    if (codexRenderMode === "chat") {
      void sessionActions.applyCodexRenderMode("terminal");
      requestAnimationFrame(() => {
        terminalRef.current?.resumeInput();
      });
      return;
    }
    terminalRef.current?.blur();
    void sessionActions.applyCodexRenderMode("chat");
  }, [codexRenderMode, sessionActions.applyCodexRenderMode, terminalRef]);

  const headerTitle =
    displayName || presentedAgent.title || agent?.name || agentId || "Session";

  // Header chrome stays on chatChrome in both chat and terminal modes so Brain
  // and Agent screens share one floating header language.
  const topBarProps = useTerminalTopBarProps({
    title: headerTitle,
    // Keep header compact: paths belong in resource details, not the chrome.
    subtitle: undefined,
    kind: presentedAgent.kind,
    terminalFlavor: presentedAgent.terminalFlavor,
    terminalTheme,
    chrome: chatChrome,
    chromeLayout,
    navigationActions,
    codexRenderMode,
    gitDiffDisabled: gitDiff.actionDisabled,
    gitDiffSummary: gitDiff.summary,
    isStructuredChatAgent,
    delegated: agent?.delegated,
    onOpenSessionDetails: openSessionDetails,
    openGitDiff,
    onToggleCodexRenderMode: handleToggleCodexRenderMode,
  });
  const viewportProps = useTerminalViewportProps({
    showCodexChat,
    sessionKey,
    serverId,
    agentId,
    agent,
    connectionState,
    connectionIssue,
    theme: showCodexChat ? chatTheme : terminalTheme,
    chrome: showCodexChat ? chatChrome : chrome,
    screenFocused,
    terminalRef,
    ctrlArmed,
    onCtrlArmedChange: handleCtrlArmedChange,
    viewportModel,
    hasTerminalRoute,
    outputBottomInset,
    accessoryBottomOffset,
    serverUrl,
    daemonId,
    keyboardVisible,
    sessionActions,
    onAccessoryLayout: handleAccessoryLayout,
  });
  const overlayProps = useTerminalScreenOverlayProps({
    resourceSheetVisible,
    resourceSheetLoading,
    resourceSheetError,
    resourceSheetSnapshot,
    creatingSession,
    menuVisible,
    menuPosition,
    connectionConnected: connectionState === "connected",
    gitDiff,
    hasLinkedWork,
    showToggleRenderMode: isStructuredChatAgent,
    toggleRenderModeLabel:
      codexRenderMode === "chat" ? "Open terminal" : "Open chat",
    newTerminalVisible,
    agentCwd: agent?.cwd,
    serverId: selectedServerId,
    renameVisible,
    renameDraft,
    renamePlaceholder,
    chrome,
    theme: terminalTheme,
    onCloseResourceSheet: closeResourceSheet,
    onRetryResourceSheet: retryResourceSheet,
    setNewTerminalVisible,
    setRenameVisible,
    setRenameDraft,
    navigationActions,
    sessionActions,
    closeMenu: chromeLayout.closeMenu,
    openNewTerminal,
    openRenameModal,
    onToggleRenderMode: handleToggleCodexRenderMode,
  });

  return {
    overlayProps,
    topBarProps,
    viewportProps,
  };
}
