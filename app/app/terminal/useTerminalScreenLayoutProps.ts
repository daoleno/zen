import type { RefObject } from "react";
import type { LayoutChangeEvent } from "react-native";
import type { useTerminalGitDiff } from "../../components/terminal/useTerminalGitDiff";
import type { TerminalSurfaceHandle } from "../../components/terminal/TerminalSurface";
import type {
  TerminalThemeChrome,
  TerminalThemeName,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { Agent, ConnectionState } from "../../store/agents";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type {
  StoredAgentAliases,
  StoredCodexRenderMode,
} from "../../services/storage";
import type { AgentDirectorySection } from "../../services/serverSelection";
import type { useTerminalScreenChrome } from "./useTerminalScreenChrome";
import type { useTerminalSessionActions } from "./useTerminalSessionActions";
import type { useTerminalTabActions } from "./useTerminalTabActions";
import type { useTerminalViewportModel } from "./useTerminalViewportModel";
import { useTerminalScreenOverlayProps } from "./useTerminalScreenOverlayProps";
import { useTerminalTopBarProps } from "./useTerminalTopBarProps";
import { useTerminalViewportProps } from "./useTerminalViewportProps";

interface UseTerminalScreenLayoutPropsInput {
  activePinned: boolean;
  agent?: Agent;
  agentAliases: StoredAgentAliases;
  agentId: string;
  accessoryBottomOffset: number;
  chrome: TerminalThemeChrome;
  chromeLayout: ReturnType<typeof useTerminalScreenChrome>;
  codexRenderMode: StoredCodexRenderMode;
  connectionIssue?: ConnectionIssue | null;
  connectionState: ConnectionState;
  creatingSession: boolean;
  ctrlArmed: boolean;
  daemonId?: string;
  gitDiff: ReturnType<typeof useTerminalGitDiff>;
  handleAccessoryLayout(event: LayoutChangeEvent): void;
  handleCtrlArmedChange(next: boolean): void;
  hasLinkedWork: boolean;
  hasTerminalRoute: boolean;
  isCodexAgent: boolean;
  keyboardVisible: boolean;
  menuPosition: { left: number; top: number };
  menuVisible: boolean;
  newTerminalVisible: boolean;
  outputBottomInset: number;
  pickerSections: AgentDirectorySection[];
  pickerVisible: boolean;
  renameDraft: string;
  renamePlaceholder: string;
  renameVisible: boolean;
  screenFocused: boolean;
  selectedServerId: string;
  serverId: string;
  serverUrl?: string;
  sessionKey: string | null;
  setNewTerminalVisible(value: boolean): void;
  setPickerVisible(value: boolean): void;
  setRenameDraft(value: string): void;
  setRenameVisible(value: boolean): void;
  showCodexChat: boolean;
  showPickerServerNames: boolean;
  sortedAgentCount: number;
  tabActions: ReturnType<typeof useTerminalTabActions>;
  tabs: Parameters<typeof useTerminalTopBarProps>[0]["tabs"];
  terminalRef: RefObject<TerminalSurfaceHandle | null>;
  terminalTheme: TerminalThemePalette;
  themeName: TerminalThemeName;
  viewportModel: ReturnType<typeof useTerminalViewportModel>;
  openGitDiff(): void;
  openNewTerminal(): void;
  openRenameModal(): void;
  sessionActions: ReturnType<typeof useTerminalSessionActions>;
}

export function useTerminalScreenLayoutProps({
  activePinned,
  agent,
  agentAliases,
  agentId,
  accessoryBottomOffset,
  chrome,
  chromeLayout,
  codexRenderMode,
  connectionIssue,
  connectionState,
  creatingSession,
  ctrlArmed,
  daemonId,
  gitDiff,
  handleAccessoryLayout,
  handleCtrlArmedChange,
  hasLinkedWork,
  hasTerminalRoute,
  isCodexAgent,
  keyboardVisible,
  menuPosition,
  menuVisible,
  newTerminalVisible,
  outputBottomInset,
  pickerSections,
  pickerVisible,
  renameDraft,
  renamePlaceholder,
  renameVisible,
  screenFocused,
  selectedServerId,
  serverId,
  serverUrl,
  sessionKey,
  setNewTerminalVisible,
  setPickerVisible,
  setRenameDraft,
  setRenameVisible,
  showCodexChat,
  showPickerServerNames,
  sortedAgentCount,
  tabActions,
  tabs,
  terminalRef,
  terminalTheme,
  themeName,
  viewportModel,
  openGitDiff,
  openNewTerminal,
  openRenameModal,
  sessionActions,
}: UseTerminalScreenLayoutPropsInput) {
  const topBarProps = useTerminalTopBarProps({
    tabs,
    terminalTheme,
    chrome,
    chromeLayout,
    tabActions,
    openNewTerminal,
  });
  const viewportProps = useTerminalViewportProps({
    showCodexChat,
    sessionKey,
    serverId,
    agentId,
    agent,
    connectionState,
    connectionIssue,
    theme: terminalTheme,
    chrome,
    themeName,
    screenFocused,
    gitDiff,
    terminalRef,
    ctrlArmed,
    onCtrlArmedChange: handleCtrlArmedChange,
    viewportModel,
    hasTerminalRoute,
    isCodexAgent,
    outputBottomInset,
    accessoryBottomOffset,
    serverUrl,
    daemonId,
    keyboardVisible,
    sessionActions,
    openGitDiff,
    onAccessoryLayout: handleAccessoryLayout,
  });
  const overlayProps = useTerminalScreenOverlayProps({
    pickerVisible,
    pickerSections,
    sortedAgentCount,
    sessionKey,
    showPickerServerNames,
    agentAliases,
    creatingSession,
    menuVisible,
    menuPosition,
    connectionConnected: connectionState === "connected",
    gitDiff,
    activePinned,
    tabs,
    isCodexAgent,
    codexRenderMode,
    hasLinkedWork,
    newTerminalVisible,
    agentCwd: agent?.cwd,
    serverId: selectedServerId,
    renameVisible,
    renameDraft,
    renamePlaceholder,
    chrome,
    theme: terminalTheme,
    setPickerVisible,
    setNewTerminalVisible,
    setRenameVisible,
    setRenameDraft,
    tabActions,
    sessionActions,
    closeMenu: chromeLayout.closeMenu,
    openNewTerminal,
    openGitDiff,
    openRenameModal,
  });

  return {
    overlayProps,
    topBarProps,
    viewportProps,
  };
}
