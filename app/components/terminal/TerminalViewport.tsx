import React from "react";
import {
  StyleSheet,
  View,
  type LayoutChangeEvent,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { Agent, ConnectionState } from "../../store/agents";
import { CodexChatSurface } from "./CodexChatSurface";
import type { CodexChatAgentInfo } from "./CodexChatSession";
import { TerminalOutputPane } from "./TerminalOutputPane";
import type { TerminalSurfaceHandle } from "./TerminalSurface";

export interface TerminalViewportProps {
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
  terminalRef: React.RefObject<TerminalSurfaceHandle | null>;
  ctrlArmed: boolean;
  onCtrlArmedChange(next: boolean): void;
  canRenderTerminal: boolean;
  shouldMountTerminalSurface: boolean;
  terminalStateAccent: string;
  terminalStateBusy: boolean;
  terminalStateTitle: string;
  terminalStateDetail: string;
  terminalStateHint: string;
  hasTerminalRoute: boolean;
  outputBottomInset: number;
  accessoryVisible: boolean;
  accessoryBottomOffset: number;
  serverUrl: string;
  daemonId: string;
  keyboardVisible: boolean;
  onSwitchToTerminal(): void;
  onOpenGitDiff(): void;
  onRetryConnection(): void;
  onAccessoryLayout(event: LayoutChangeEvent): void;
}

export function TerminalViewport({
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
  terminalRef,
  ctrlArmed,
  onCtrlArmedChange,
  canRenderTerminal,
  shouldMountTerminalSurface,
  terminalStateAccent,
  terminalStateBusy,
  terminalStateTitle,
  terminalStateDetail,
  terminalStateHint,
  hasTerminalRoute,
  outputBottomInset,
  accessoryVisible,
  accessoryBottomOffset,
  serverUrl,
  daemonId,
  keyboardVisible,
  onSwitchToTerminal,
  onOpenGitDiff,
  onRetryConnection,
  onAccessoryLayout,
}: TerminalViewportProps) {
  const codexChatAgentInfo = React.useMemo<CodexChatAgentInfo | undefined>(
    () =>
      agent
        ? {
            cwd: agent.cwd,
            command: agent.command,
            name: agent.name,
            startedAt: agent.started_at,
            processId: agent.process_id,
          }
        : undefined,
    [
      agent?.command,
      agent?.cwd,
      agent?.name,
      agent?.process_id,
      agent?.started_at,
    ],
  );

  return (
    <View style={[styles.terminalStage, { backgroundColor: theme.background }]}>
      <View style={[styles.terminalShell, { backgroundColor: theme.background }]}>
        <View style={styles.terminalContent}>
          <TerminalOutputPane
            sessionKey={sessionKey}
            serverId={serverId}
            agentId={agentId}
            theme={theme}
            chrome={chrome}
            terminalRef={terminalRef}
            ctrlArmed={ctrlArmed}
            onCtrlArmedChange={onCtrlArmedChange}
            canRenderTerminal={canRenderTerminal}
            shouldMountTerminalSurface={shouldMountTerminalSurface}
            terminalStateAccent={terminalStateAccent}
            terminalStateBusy={terminalStateBusy}
            terminalStateTitle={terminalStateTitle}
            terminalStateDetail={terminalStateDetail}
            terminalStateHint={terminalStateHint}
            hasTerminalRoute={hasTerminalRoute}
            outputBottomInset={outputBottomInset}
            accessoryVisible={accessoryVisible}
            accessoryBottomOffset={accessoryBottomOffset}
            serverUrl={serverUrl}
            daemonId={daemonId}
            keyboardVisible={keyboardVisible}
            onRetryConnection={onRetryConnection}
            onAccessoryLayout={onAccessoryLayout}
          />

          {sessionKey && serverId && agentId ? (
            <View
              pointerEvents={showCodexChat ? "auto" : "none"}
              style={[
                styles.chatOverlay,
                !showCodexChat ? styles.chatOverlayHidden : null,
              ]}
            >
              <CodexChatSurface
                key={`codex-chat:${sessionKey}`}
                visible={showCodexChat}
                serverId={serverId}
                agentId={agentId}
                agentInfo={codexChatAgentInfo}
                connectionState={connectionState}
                connectionIssue={connectionIssue}
                theme={theme}
                chrome={chrome}
                screenFocused={screenFocused}
                onSwitchToTerminal={onSwitchToTerminal}
                onOpenGitDiff={onOpenGitDiff}
              />
            </View>
          ) : null}
        </View>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  terminalStage: {
    flex: 1,
    minHeight: 0,
    overflow: "hidden",
    justifyContent: "center",
  },
  terminalShell: {
    flex: 1,
    minHeight: 0,
  },
  terminalContent: {
    flex: 1,
    minHeight: 0,
    position: "relative",
  },
  chatOverlay: {
    position: "absolute",
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
    zIndex: 12,
  },
  chatOverlayHidden: {
    opacity: 0,
  },
});
