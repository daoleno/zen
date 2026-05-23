import React from "react";
import {
  StyleSheet,
  View,
  type LayoutChangeEvent,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemeName,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { Agent, ConnectionState } from "../../store/agents";
import { CodexChatSurface } from "./CodexChatSurface";
import { TerminalOutputPane } from "./TerminalOutputPane";
import type { TerminalSurfaceHandle } from "./TerminalSurface";

interface GitDiffChip {
  label: string;
  tone: "clean" | "dirty" | "error" | "loading";
  onPress(): void;
}

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
  themeName: TerminalThemeName;
  screenFocused: boolean;
  gitDiff?: GitDiffChip | null;
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
  isCodexAgent: boolean;
  outputBottomInset: number;
  accessoryVisible: boolean;
  accessoryBottomOffset: number;
  serverUrl: string;
  daemonId: string;
  keyboardVisible: boolean;
  onSwitchToTerminal(): void;
  onSwitchToChat(): void;
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
  themeName,
  screenFocused,
  gitDiff,
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
  isCodexAgent,
  outputBottomInset,
  accessoryVisible,
  accessoryBottomOffset,
  serverUrl,
  daemonId,
  keyboardVisible,
  onSwitchToTerminal,
  onSwitchToChat,
  onOpenGitDiff,
  onRetryConnection,
  onAccessoryLayout,
}: TerminalViewportProps) {
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
            themeName={themeName}
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
            isCodexAgent={isCodexAgent}
            outputBottomInset={outputBottomInset}
            accessoryVisible={accessoryVisible}
            accessoryBottomOffset={accessoryBottomOffset}
            serverUrl={serverUrl}
            daemonId={daemonId}
            keyboardVisible={keyboardVisible}
            gitDiff={gitDiff}
            chatOverlayVisible={showCodexChat}
            onSwitchToChat={onSwitchToChat}
            onRetryConnection={onRetryConnection}
            onAccessoryLayout={onAccessoryLayout}
          />

          {showCodexChat && sessionKey && serverId && agentId ? (
            <View style={styles.chatOverlay}>
              <CodexChatSurface
                key={`codex-chat:${sessionKey}`}
                serverId={serverId}
                agentId={agentId}
                agent={agent}
                connectionState={connectionState}
                connectionIssue={connectionIssue}
                theme={theme}
                chrome={chrome}
                screenFocused={screenFocused}
                gitDiff={gitDiff}
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
});
