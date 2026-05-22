import { useCallback, useMemo } from "react";
import type { AgentDirectorySection } from "../../services/serverSelection";
import type {
  StoredAgentAliases,
  StoredCodexRenderMode,
} from "../../services/storage";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { TerminalTabDescriptor } from "../../components/terminal/TerminalTopBar";
import type { useTerminalGitDiff } from "../../components/terminal/useTerminalGitDiff";
import type { TerminalScreenOverlaysProps } from "./TerminalScreenOverlays";
import type { useTerminalSessionActions } from "./useTerminalSessionActions";
import type { useTerminalTabActions } from "./useTerminalTabActions";

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
  activePinned: boolean;
  tabs: TerminalTabDescriptor[];
  isCodexAgent: boolean;
  codexRenderMode: StoredCodexRenderMode;
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
  tabActions: ReturnType<typeof useTerminalTabActions>;
  sessionActions: ReturnType<typeof useTerminalSessionActions>;
  closeMenu(): void;
  openNewTerminal(): void;
  openGitDiff(): void;
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
  activePinned,
  tabs,
  isCodexAgent,
  codexRenderMode,
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
  tabActions,
  sessionActions,
  closeMenu,
  openNewTerminal,
  openGitDiff,
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
      gitDiffDisabled: gitDiff.actionDisabled,
      activePinned,
      closeOtherTabsDisabled: tabs.length <= 1,
      isCodexAgent,
      codexRenderMode,
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
      onOpenAgent: tabActions.openAgentTab,
      onNewTerminal: openNewTerminal,
      onCloseMenu: closeMenu,
      onOpenGitDiff: openGitDiff,
      onRename: openRenameModal,
      onTogglePinned: tabActions.handleTogglePinned,
      onCloseOtherTabs: tabActions.handleCloseOtherTabs,
      onCloseTab: tabActions.handleCloseCurrentTab,
      onOpenLinkedWork: sessionActions.openLinkedWork,
      onTerminate: tabActions.handleTerminateAgent,
      onToggleCodexRenderMode: sessionActions.toggleCodexRenderMode,
      onCloseNewTerminal: handleCloseNewTerminal,
      onSubmitNewTerminal: handleSubmitNewTerminal,
      onRenameDraftChange: setRenameDraft,
      onCloseRename: handleCloseRename,
      onSaveRename: sessionActions.handleSaveRename,
    }),
    [
      activePinned,
      agentAliases,
      agentCwd,
      chrome,
      codexRenderMode,
      connectionConnected,
      creatingSession,
      gitDiff.actionDisabled,
      gitDiff.sheetProps,
      handleCloseNewTerminal,
      handleClosePicker,
      handleCloseRename,
      handleSubmitNewTerminal,
      hasLinkedWork,
      isCodexAgent,
      menuPosition,
      menuVisible,
      newTerminalVisible,
      closeMenu,
      openGitDiff,
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
      sessionActions.toggleCodexRenderMode,
      sessionKey,
      setRenameDraft,
      showPickerServerNames,
      sortedAgentCount,
      tabActions.handleCloseCurrentTab,
      tabActions.handleCloseOtherTabs,
      tabActions.handleTerminateAgent,
      tabActions.handleTogglePinned,
      tabActions.openAgentTab,
      tabs.length,
      theme,
    ],
  );
}
