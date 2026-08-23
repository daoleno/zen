import { useCallback, useMemo } from "react";
import type { SessionResourceSnapshot } from "../../../services/sessionResourceSnapshot";
import type {
  ProviderError,
  ThreadRuntimeChoice,
} from "../../../services/providers";
import type { ProviderPickerModelRow } from "../../../services/providers/sessionModelHelpers";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../../constants/terminalThemes";
import type { useTerminalGitDiff } from "../useTerminalGitDiff";
import type { TerminalScreenOverlaysProps } from "./TerminalScreenOverlays";
import type { useTerminalSessionActions } from "./useTerminalSessionActions";
import type { useTerminalNavigationActions } from "./useTerminalNavigationActions";

interface UseTerminalScreenOverlayPropsInput {
  resourceSheetVisible: boolean;
  resourceSheetTitle: string;
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
  creatingSession: boolean;
  menuVisible: boolean;
  menuPosition: { left: number; top: number };
  connectionConnected: boolean;
  gitDiff: ReturnType<typeof useTerminalGitDiff>;
  hasLinkedWork: boolean;
  showToggleRenderMode?: boolean;
  toggleRenderModeLabel?: string;
  newTerminalVisible: boolean;
  agentCwd?: string;
  serverId: string;
  renameVisible: boolean;
  renameDraft: string;
  renamePlaceholder: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onCloseResourceSheet(): void;
  onRetryResourceSheet(): void;
  onCloseRouteSheet(): void;
  onRetryRouteSheet(): void;
  onActivateSessionModel(choice: ThreadRuntimeChoice): void;
  onOpenModel?(): void;
  setNewTerminalVisible(value: boolean): void;
  setRenameVisible(value: boolean): void;
  setRenameDraft(value: string): void;
  navigationActions: ReturnType<typeof useTerminalNavigationActions>;
  sessionActions: ReturnType<typeof useTerminalSessionActions>;
  closeMenu(): void;
  openNewTerminal(): void;
  openRenameModal(): void;
  onToggleRenderMode?(): void;
}

export function useTerminalScreenOverlayProps({
  resourceSheetVisible,
  resourceSheetTitle,
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
  connectionConnected,
  gitDiff,
  hasLinkedWork,
  showToggleRenderMode = false,
  toggleRenderModeLabel,
  newTerminalVisible,
  agentCwd,
  serverId,
  renameVisible,
  renameDraft,
  renamePlaceholder,
  chrome,
  theme,
  onCloseResourceSheet,
  onRetryResourceSheet,
  onCloseRouteSheet,
  onRetryRouteSheet,
  onActivateSessionModel,
  onOpenModel,
  setNewTerminalVisible,
  setRenameVisible,
  setRenameDraft,
  navigationActions,
  sessionActions,
  closeMenu,
  openNewTerminal,
  openRenameModal,
  onToggleRenderMode,
}: UseTerminalScreenOverlayPropsInput): TerminalScreenOverlaysProps {
  const handleCloseNewTerminal = useCallback(() => {
    setNewTerminalVisible(false);
  }, [setNewTerminalVisible]);

  const handleSubmitNewTerminal = useCallback(
    (input: Parameters<TerminalScreenOverlaysProps["onSubmitNewTerminal"]>[0]) => {
      void sessionActions.createTerminal({
        cwd: input.cwd,
        command: input.command,
        name: input.name,
      });
    },
    [sessionActions.createTerminal],
  );

  const handleCloseRename = useCallback(() => {
    setRenameVisible(false);
  }, [setRenameVisible]);

  return useMemo(
    () => ({
      resourceSheetVisible,
      resourceSheetTitle,
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
      newTerminalDisabled: !connectionConnected,
      showLinkedWork: hasLinkedWork,
      showToggleRenderMode,
      toggleRenderModeLabel,
      newTerminalVisible,
      newTerminalInitialCwd: agentCwd || "",
      selectedServerId: serverId,
      gitDiffSheetProps: gitDiff.sheetProps,
      renameVisible,
      renameDraft,
      renamePlaceholder,
      chrome,
      theme,
      onCloseResourceSheet,
      onRetryResourceSheet,
      onCloseRouteSheet,
      onRetryRouteSheet,
      onActivateSessionModel,
      onOpenModel,
      onNewTerminal: openNewTerminal,
      onCloseMenu: closeMenu,
      onRename: openRenameModal,
      onOpenLinkedWork: sessionActions.openLinkedWork,
      onToggleRenderMode,
      onTerminate: navigationActions.handleTerminateAgent,
      onCloseNewTerminal: handleCloseNewTerminal,
      onSubmitNewTerminal: handleSubmitNewTerminal,
      onRenameDraftChange: setRenameDraft,
      onCloseRename: handleCloseRename,
      onSaveRename: sessionActions.handleSaveRename,
    }),
    [
      agentCwd,
      chrome,
      closeMenu,
      connectionConnected,
      creatingSession,
      gitDiff.sheetProps,
      handleCloseNewTerminal,
      handleCloseRename,
      handleSubmitNewTerminal,
      hasLinkedWork,
      menuPosition,
      menuVisible,
      navigationActions.handleTerminateAgent,
      newTerminalVisible,
      onActivateSessionModel,
      onCloseResourceSheet,
      onCloseRouteSheet,
      onOpenModel,
      onRetryResourceSheet,
      onRetryRouteSheet,
      onToggleRenderMode,
      openNewTerminal,
      openRenameModal,
      renameDraft,
      renamePlaceholder,
      renameVisible,
      resourceSheetError,
      resourceSheetLoading,
      resourceSheetSnapshot,
      resourceSheetTitle,
      resourceSheetVisible,
      routeSheetActivating,
      routeSheetError,
      routeSheetRows,
      routeSheetLoading,
      routeSheetVisible,
      serverId,
      sessionActions.handleSaveRename,
      sessionActions.openLinkedWork,
      setRenameDraft,
      showToggleRenderMode,
      theme,
      toggleRenderModeLabel,
    ],
  );
}
