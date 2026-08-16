import { useCallback, useMemo } from "react";
import type { LayoutChangeEvent } from "react-native";
import type { ConnectionIssue } from "../../../services/connectionIssue";
import type { ComposerModelControlPresentation } from "../../../services/providers/sessionModelHelpers";
import type { TerminalSurfaceHandle } from "../TerminalSurface";
import type { TerminalViewportProps } from "../TerminalViewport";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../../constants/terminalThemes";
import type { Agent, ConnectionState } from "../../../store/agents";
import type { useTerminalSessionActions } from "./useTerminalSessionActions";
import type { useTerminalViewportModel } from "./useTerminalViewportModel";

interface UseTerminalViewportPropsInput {
  showInterfaceChat: boolean;
  initialComposerFocusGrant: string | null;
  sessionKey: string | null;
  serverId: string;
  agentId: string;
  agent?: Agent;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  screenFocused: boolean;
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
  onAccessoryLayout(event: LayoutChangeEvent): void;
  onConsumeInitialComposerFocus(): void;
  composerModelControl?: ComposerModelControlPresentation | null;
  onComposerModelControlPress?: () => void;
}

export function useTerminalViewportProps({
  showInterfaceChat,
  initialComposerFocusGrant,
  sessionKey,
  serverId,
  agentId,
  agent,
  connectionState,
  connectionIssue,
  theme,
  chrome,
  screenFocused,
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
  onAccessoryLayout,
  onConsumeInitialComposerFocus,
  composerModelControl,
  onComposerModelControlPress,
}: UseTerminalViewportPropsInput): TerminalViewportProps {
  const handleSwitchToTerminal = useCallback(() => {
    void sessionActions.applyInterfaceRenderMode("terminal");
    requestAnimationFrame(() => {
      terminalRef.current?.resumeInput();
    });
  }, [sessionActions.applyInterfaceRenderMode, terminalRef]);

  const handleRetryConnection = useCallback(() => {
    void sessionActions.retryServerConnection();
  }, [sessionActions.retryServerConnection]);

  return useMemo(
    () => ({
      showInterfaceChat,
      initialComposerFocusGrant,
      sessionKey,
      serverId,
      agentId,
      agent,
      connectionState,
      connectionIssue,
      theme,
      chrome,
      screenFocused,
      terminalRef,
      ctrlArmed,
      onCtrlArmedChange,
      canRenderTerminal: viewportModel.canRenderTerminal,
      shouldMountTerminalSurface: viewportModel.shouldMountTerminalSurface,
      terminalSurfaceActive: viewportModel.terminalSurfaceActive,
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
      onRetryConnection: handleRetryConnection,
      onAccessoryLayout,
      onConsumeInitialComposerFocus,
      composerModelControl,
      onComposerModelControlPress,
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
      handleRetryConnection,
      handleSwitchToTerminal,
      hasTerminalRoute,
      keyboardVisible,
      onAccessoryLayout,
      onConsumeInitialComposerFocus,
      onCtrlArmedChange,
      outputBottomInset,
      screenFocused,
      serverId,
      serverUrl,
      sessionKey,
      terminalRef,
      theme,
      showInterfaceChat,
      composerModelControl,
      onComposerModelControlPress,
      viewportModel.accessoryVisible,
      viewportModel.canRenderTerminal,
      viewportModel.shouldMountTerminalSurface,
      viewportModel.terminalSurfaceActive,
      viewportModel.terminalState.accent,
      viewportModel.terminalState.busy,
      viewportModel.terminalState.detail,
      viewportModel.terminalState.hint,
      viewportModel.terminalState.title,
    ],
  );
}
