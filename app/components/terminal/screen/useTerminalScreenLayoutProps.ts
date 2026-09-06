import { useCallback, type RefObject } from "react";
import type { LayoutChangeEvent } from "react-native";
import type { useTerminalGitDiff } from "../useTerminalGitDiff";
import type { TerminalSurfaceHandle } from "../TerminalSurface";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../../constants/terminalThemes";
import type { Worker, ConnectionState } from "../../../store/workers";
import type { ConnectionIssue } from "../../../services/connectionIssue";
import type { StoredInterfaceRenderMode } from "../../../services/storage";
import type { PresentedWorker } from "../../../services/workerPresentation";
import type { SessionResourceSnapshot } from "../../../services/sessionResourceSnapshot";
import type {
  ProviderError,
  ThreadRuntimeChoice,
} from "../../../services/providers";
import type { ProviderPickerModelRow } from "../../../services/providers/sessionModelHelpers";
import type { ComposerModelControlPresentation } from "../../../services/providers/sessionModelHelpers";
import type { useTerminalScreenChrome } from "./useTerminalScreenChrome";
import type { useTerminalSessionActions } from "./useTerminalSessionActions";
import type { useTerminalNavigationActions } from "./useTerminalNavigationActions";
import type { useTerminalViewportModel } from "./useTerminalViewportModel";
import { useTerminalScreenOverlayProps } from "./useTerminalScreenOverlayProps";
import { useTerminalTopBarProps } from "./useTerminalTopBarProps";
import { useTerminalViewportProps } from "./useTerminalViewportProps";

interface UseTerminalScreenLayoutPropsInput {
  agent?: Worker;
  workerId: string;
  accessoryBottomOffset: number;
  chrome: TerminalThemeChrome;
  chatChrome: TerminalThemeChrome;
  chatTheme: TerminalThemePalette;
  chromeLayout: ReturnType<typeof useTerminalScreenChrome>;
  interfaceRenderMode: StoredInterfaceRenderMode;
  initialComposerFocusGrant: string | null;
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
  isStructuredChatWorker: boolean;
  keyboardVisible: boolean;
  menuPosition: { left: number; top: number };
  menuVisible: boolean;
  newTerminalVisible: boolean;
  outputBottomInset: number;
  presentedWorker: PresentedWorker;
  renameDraft: string;
  renamePlaceholder: string;
  renameVisible: boolean;
  resourceSheetVisible: boolean;
  resourceSheetLoading: boolean;
  resourceSheetError?: string | null;
  resourceSheetSnapshot?: SessionResourceSnapshot | null;
  routeSheetVisible: boolean;
  routeSheetLoading: boolean;
  routeSheetActivating: boolean;
  routeSheetError?: ProviderError | string | null;
  routeSheetRows: ProviderPickerModelRow[];
  createDurabilityWarning?: string | null;
  onDismissCreateDurabilityWarning?(): void;
  screenFocused: boolean;
  selectedServerId: string;
  serverId: string;
  serverUrl?: string;
  sessionKey: string | null;
  setNewTerminalVisible(value: boolean): void;
  setRenameDraft(value: string): void;
  setRenameVisible(value: boolean): void;
  showInterfaceChat: boolean;
  onConsumeInitialComposerFocus(): void;
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
  openModel?: () => void;
  modelActionAvailable?: boolean;
  composerModelControl?: ComposerModelControlPresentation | null;
  onComposerModelControlPress?: () => void;
  closeRouteSheet(): void;
  retryRouteSheet(): void;
  activateSessionModel(choice: ThreadRuntimeChoice): void;
  sessionActions: ReturnType<typeof useTerminalSessionActions>;
}

export function useTerminalScreenLayoutProps({
  agent,
  workerId,
  accessoryBottomOffset,
  chrome,
  chatChrome,
  chatTheme,
  chromeLayout,
  interfaceRenderMode,
  initialComposerFocusGrant,
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
  isStructuredChatWorker,
  keyboardVisible,
  menuPosition,
  menuVisible,
  newTerminalVisible,
  outputBottomInset,
  presentedWorker,
  renameDraft,
  renamePlaceholder,
  renameVisible,
  resourceSheetVisible,
  resourceSheetLoading,
  resourceSheetError,
  resourceSheetSnapshot,
  routeSheetVisible,
  routeSheetLoading,
  routeSheetActivating,
  routeSheetError,
  routeSheetRows,
  createDurabilityWarning,
  onDismissCreateDurabilityWarning,
  screenFocused,
  selectedServerId,
  serverId,
  serverUrl,
  sessionKey,
  setNewTerminalVisible,
  setRenameDraft,
  setRenameVisible,
  showInterfaceChat,
  onConsumeInitialComposerFocus,
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
  openModel,
  modelActionAvailable = false,
  composerModelControl,
  onComposerModelControlPress,
  closeRouteSheet,
  retryRouteSheet,
  activateSessionModel,
  sessionActions,
}: UseTerminalScreenLayoutPropsInput) {
  const handleToggleInterfaceRenderMode = useCallback(() => {
    if (interfaceRenderMode === "chat") {
      void sessionActions.applyInterfaceRenderMode("terminal");
      requestAnimationFrame(() => {
        terminalRef.current?.resumeInput();
      });
      return;
    }
    terminalRef.current?.blur();
    void sessionActions.applyInterfaceRenderMode("chat");
  }, [
    interfaceRenderMode,
    sessionActions.applyInterfaceRenderMode,
    terminalRef,
  ]);

  const headerTitle =
    displayName || presentedWorker.title || agent?.name || workerId || "Session";

  // Header chrome stays on chatChrome in both chat and terminal modes so Brain
  // and Agent screens share one floating header language.
  const topBarProps = useTerminalTopBarProps({
    title: headerTitle,
    // Keep header compact: paths belong in resource details, not the chrome.
    subtitle: undefined,
    kind: presentedWorker.kind,
    terminalFlavor: presentedWorker.terminalFlavor,
    terminalTheme,
    chrome: chatChrome,
    chromeLayout,
    navigationActions,
    interfaceRenderMode,
    gitDiffDisabled: gitDiff.actionDisabled,
    gitDiffSummary: gitDiff.summary,
    isStructuredChatWorker,
    delegated: agent?.delegated,
    onOpenSessionDetails: openSessionDetails,
    openGitDiff,
    onToggleInterfaceRenderMode: handleToggleInterfaceRenderMode,
  });
  const viewportProps = useTerminalViewportProps({
    showInterfaceChat,
    initialComposerFocusGrant,
    sessionKey,
    serverId,
    workerId,
    agent,
    connectionState,
    connectionIssue,
    theme: showInterfaceChat ? chatTheme : terminalTheme,
    chrome: showInterfaceChat ? chatChrome : chrome,
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
    onConsumeInitialComposerFocus,
    composerModelControl,
    onComposerModelControlPress,
  });
  const overlayProps = useTerminalScreenOverlayProps({
    resourceSheetVisible,
    resourceSheetTitle: headerTitle,
    resourceSheetLoading,
    resourceSheetError,
    resourceSheetSnapshot,
    routeSheetVisible,
    routeSheetLoading,
    routeSheetActivating,
    routeSheetError,
    routeSheetRows,
    createDurabilityWarning,
    onDismissCreateDurabilityWarning,
    creatingSession,
    menuVisible,
    menuPosition,
    connectionConnected: connectionState === "connected",
    gitDiff,
    hasLinkedWork,
    showToggleRenderMode: isStructuredChatWorker,
    toggleRenderModeLabel:
      interfaceRenderMode === "chat" ? "Open terminal" : "Open chat",
    newTerminalVisible,
    workerCwd: agent?.cwd,
    serverId: selectedServerId,
    renameVisible,
    renameDraft,
    renamePlaceholder,
    chrome,
    theme: terminalTheme,
    onCloseResourceSheet: closeResourceSheet,
    onRetryResourceSheet: retryResourceSheet,
    onCloseRouteSheet: closeRouteSheet,
    onRetryRouteSheet: retryRouteSheet,
    onActivateSessionModel: activateSessionModel,
    onOpenModel: modelActionAvailable
      ? openModel
      : undefined,
    setNewTerminalVisible,
    setRenameVisible,
    setRenameDraft,
    navigationActions,
    sessionActions,
    closeMenu: chromeLayout.closeMenu,
    openNewTerminal,
    openRenameModal,
    onToggleRenderMode: handleToggleInterfaceRenderMode,
  });

  return {
    overlayProps,
    topBarProps,
    viewportProps,
  };
}
