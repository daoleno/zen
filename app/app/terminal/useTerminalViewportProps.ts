import { useCallback, useMemo } from "react";
import type { LayoutChangeEvent } from "react-native";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { TerminalSurfaceHandle } from "../../components/terminal/TerminalSurface";
import type { TerminalViewportProps } from "../../components/terminal/TerminalViewport";
import type { useTerminalGitDiff } from "../../components/terminal/useTerminalGitDiff";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { Agent, ConnectionState } from "../../store/agents";
import type { useTerminalSessionActions } from "./useTerminalSessionActions";
import type { useTerminalViewportModel } from "./useTerminalViewportModel";

interface UseTerminalViewportPropsInput {
  showCodexChat: boolean;
  sessionKey: string | null;
  serverId: string;
  agentId: string;
  agent?: Agent;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  screenFocused: boolean;
  gitDiff: ReturnType<typeof useTerminalGitDiff>;
  terminalRef: React.RefObject<TerminalSurfaceHandle | null>;
  ctrlArmed: boolean;
  onCtrlArmedChange(next: boolean): void;
  viewportModel: ReturnType<typeof useTerminalViewportModel>;
  hasTerminalRoute: boolean;
  outputBottomInset: number;
  accessoryBottomOffset: number;
  serverUrl?: string;
  daemonId?: string;
  keyboardVisible: boolean;
  sessionActions: ReturnType<typeof useTerminalSessionActions>;
  openGitDiff(): void;
  onAccessoryLayout(event: LayoutChangeEvent): void;
}

export function useTerminalViewportProps({
  showCodexChat,
  sessionKey,
  serverId,
  agentId,
  agent,
  connectionState,
  connectionIssue,
  theme,
  chrome,
  screenFocused,
  gitDiff,
  terminalRef,
  ctrlArmed,
  onCtrlArmedChange,
  viewportModel,
  hasTerminalRoute,
  outputBottomInset,
  accessoryBottomOffset,
  serverUrl,
  daemonId,
  keyboardVisible,
  sessionActions,
  openGitDiff,
  onAccessoryLayout,
}: UseTerminalViewportPropsInput): TerminalViewportProps {
  const handleSwitchToTerminal = useCallback(() => {
    void sessionActions.applyCodexRenderMode("terminal");
    requestAnimationFrame(() => {
      terminalRef.current?.resumeInput();
    });
  }, [sessionActions.applyCodexRenderMode, terminalRef]);

  const handleRetryConnection = useCallback(() => {
    void sessionActions.retryServerConnection();
  }, [sessionActions.retryServerConnection]);

  return useMemo(
    () => ({
      showCodexChat,
      sessionKey,
      serverId,
      agentId,
      agent,
      connectionState,
      connectionIssue,
      theme,
      chrome,
      screenFocused,
      gitDiff: gitDiff.chip,
      terminalRef,
      ctrlArmed,
      onCtrlArmedChange,
      canRenderTerminal: viewportModel.canRenderTerminal,
      shouldMountTerminalSurface: viewportModel.shouldMountTerminalSurface,
      terminalStateAccent: viewportModel.terminalState.accent,
      terminalStateBusy: viewportModel.terminalState.busy,
      terminalStateTitle: viewportModel.terminalState.title,
      terminalStateDetail: viewportModel.terminalState.detail,
      terminalStateHint: viewportModel.terminalState.hint,
      hasTerminalRoute,
      outputBottomInset,
      accessoryVisible: viewportModel.accessoryVisible,
      accessoryBottomOffset,
      serverUrl: serverUrl || "",
      daemonId: daemonId || "",
      keyboardVisible,
      onSwitchToTerminal: handleSwitchToTerminal,
      onOpenGitDiff: openGitDiff,
      onRetryConnection: handleRetryConnection,
      onAccessoryLayout,
    }),
    [
      accessoryBottomOffset,
      agent,
      agentId,
      chrome,
      connectionIssue,
      connectionState,
      ctrlArmed,
      daemonId,
      gitDiff.chip,
      handleRetryConnection,
      handleSwitchToTerminal,
      hasTerminalRoute,
      keyboardVisible,
      onAccessoryLayout,
      onCtrlArmedChange,
      openGitDiff,
      outputBottomInset,
      screenFocused,
      serverId,
      serverUrl,
      sessionKey,
      terminalRef,
      theme,
      showCodexChat,
      viewportModel.accessoryVisible,
      viewportModel.canRenderTerminal,
      viewportModel.shouldMountTerminalSurface,
      viewportModel.terminalState.accent,
      viewportModel.terminalState.busy,
      viewportModel.terminalState.detail,
      viewportModel.terminalState.hint,
      viewportModel.terminalState.title,
    ],
  );
}
