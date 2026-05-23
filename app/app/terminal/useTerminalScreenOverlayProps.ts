import { useCallback, useMemo } from "react";
import type { AgentDirectorySection } from "../../services/serverSelection";
import type {
  StoredAgentAliases,
} from "../../services/storage";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { useTerminalGitDiff } from "../../components/terminal/useTerminalGitDiff";
import type { TerminalScreenOverlaysProps } from "./TerminalScreenOverlays";
import type { useTerminalSessionActions } from "./useTerminalSessionActions";
import type { useTerminalNavigationActions } from "./useTerminalNavigationActions";

interface UseTerminalScreenOverlayPropsInput {
  pickerVisible: boolean;
  pickerSections: AgentDirectorySection[];
  sortedAgentCount: number;
  sessionKey: string | null;
  showPickerServerNames: boolean;
  agentAliases: StoredAgentAliases;
  creatingSession: boolean;
  menuVisible: boolean;
  menuPosition: { left: number; top: number };
  connectionConnected: boolean;
  gitDiff: ReturnType<typeof useTerminalGitDiff>;
  hasLinkedWork: boolean;
  newTerminalVisible: boolean;
  agentCwd?: string;
  serverId: string;
  renameVisible: boolean;
  renameDraft: string;
  renamePlaceholder: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  setPickerVisible(value: boolean): void;
  setNewTerminalVisible(value: boolean): void;
  setRenameVisible(value: boolean): void;
  setRenameDraft(value: string): void;
  navigationActions: ReturnType<typeof useTerminalNavigationActions>;
  sessionActions: ReturnType<typeof useTerminalSessionActions>;
  closeMenu(): void;
  openNewTerminal(): void;
  openRenameModal(): void;
}

export function useTerminalScreenOverlayProps({
  pickerVisible,
  pickerSections,
  sortedAgentCount,
  sessionKey,
  showPickerServerNames,
  agentAliases,
  creatingSession,
  menuVisible,
  menuPosition,
  connectionConnected,
  gitDiff,
  hasLinkedWork,
  newTerminalVisible,
  agentCwd,
  serverId,
  renameVisible,
  renameDraft,
  renamePlaceholder,
  chrome,
  theme,
  setPickerVisible,
  setNewTerminalVisible,
  setRenameVisible,
  setRenameDraft,
  navigationActions,
  sessionActions,
  closeMenu,
  openNewTerminal,
  openRenameModal,
}: UseTerminalScreenOverlayPropsInput): TerminalScreenOverlaysProps {
  const handleClosePicker = useCallback(() => {
    setPickerVisible(false);
  }, [setPickerVisible]);

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
      pickerVisible,
      pickerSections,
      pickerAgentCount: sortedAgentCount,
      activeSessionKey: sessionKey,
      showPickerServerNames,
      agentAliases,
      creatingSession,
      menuVisible,
      menuPosition,
      newTerminalDisabled: !connectionConnected,
      showLinkedWork: hasLinkedWork,
      newTerminalVisible,
      newTerminalInitialCwd: agentCwd || "",
      selectedServerId: serverId,
      gitDiffSheetProps: gitDiff.sheetProps,
      renameVisible,
      renameDraft,
      renamePlaceholder,
      chrome,
      theme,
      onClosePicker: handleClosePicker,
      onOpenAgent: navigationActions.openAgentSession,
      onNewTerminal: openNewTerminal,
      onCloseMenu: closeMenu,
      onRename: openRenameModal,
      onOpenLinkedWork: sessionActions.openLinkedWork,
      onTerminate: navigationActions.handleTerminateAgent,
      onCloseNewTerminal: handleCloseNewTerminal,
      onSubmitNewTerminal: handleSubmitNewTerminal,
      onRenameDraftChange: setRenameDraft,
      onCloseRename: handleCloseRename,
      onSaveRename: sessionActions.handleSaveRename,
    }),
    [
      agentAliases,
      agentCwd,
      chrome,
      connectionConnected,
      creatingSession,
      gitDiff.sheetProps,
      handleCloseNewTerminal,
      handleClosePicker,
      handleCloseRename,
      handleSubmitNewTerminal,
      hasLinkedWork,
      menuPosition,
      menuVisible,
      newTerminalVisible,
      closeMenu,
      openNewTerminal,
      openRenameModal,
      pickerSections,
      pickerVisible,
      renameDraft,
      renamePlaceholder,
      renameVisible,
      serverId,
      sessionActions.handleSaveRename,
      sessionActions.openLinkedWork,
      sessionKey,
      setRenameDraft,
      showPickerServerNames,
      sortedAgentCount,
      navigationActions.handleTerminateAgent,
      navigationActions.openAgentSession,
      theme,
    ],
  );
}
