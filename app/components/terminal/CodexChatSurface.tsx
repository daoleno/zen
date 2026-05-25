import React from "react";
import {
  StyleSheet,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { ConnectionState } from "../../store/agents";
import type { ConnectionIssue } from "../../services/connectionIssue";
import { CodexChatBody } from "./CodexChatBody";
import { useCodexChatSurfaceState } from "./useCodexChatSurfaceState";
import type { CodexChatAgentInfo } from "./CodexChatSession";

interface CodexChatSurfaceProps {
  visible: boolean;
  serverId: string;
  agentId: string;
  agentInfo?: CodexChatAgentInfo;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  screenFocused: boolean;
  onSwitchToTerminal(): void;
  onOpenGitDiff(): void;
}

function CodexChatSurfaceImpl({
  visible,
  serverId,
  agentId,
  agentInfo,
  connectionState,
  connectionIssue,
  theme,
  chrome,
  screenFocused,
  onSwitchToTerminal,
  onOpenGitDiff,
}: CodexChatSurfaceProps) {
  const { bodyProps } = useCodexChatSurfaceState({
    visible,
    serverId,
    agentId,
    agentInfo,
    connectionState,
    connectionIssue,
    theme,
    chrome,
    screenFocused,
    onSwitchToTerminal,
    onOpenGitDiff,
  });

  return (
    <View
      style={[styles.root, { backgroundColor: theme.background }]}
    >
      <CodexChatBody {...bodyProps} />
    </View>
  );
}

export const CodexChatSurface = React.memo(CodexChatSurfaceImpl);

const styles = StyleSheet.create({
  root: {
    flex: 1,
    minHeight: 0,
    position: "relative",
  },
});
