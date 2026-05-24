import React from "react";
import {
  StyleSheet,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { Agent, ConnectionState } from "../../store/agents";
import type { ConnectionIssue } from "../../services/connectionIssue";
import { CodexChatBody } from "./CodexChatBody";
import { useCodexChatSurfaceState } from "./useCodexChatSurfaceState";

interface CodexChatSurfaceProps {
  serverId: string;
  agentId: string;
  agent?: Agent;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  screenFocused: boolean;
  onSwitchToTerminal(): void;
}

export function CodexChatSurface({
  serverId,
  agentId,
  agent,
  connectionState,
  connectionIssue,
  theme,
  chrome,
  screenFocused,
  onSwitchToTerminal,
}: CodexChatSurfaceProps) {
  const { bodyProps } = useCodexChatSurfaceState({
    serverId,
    agentId,
    agent,
    connectionState,
    connectionIssue,
    theme,
    chrome,
    screenFocused,
    onSwitchToTerminal,
  });

  return (
    <View
      style={[styles.root, { backgroundColor: theme.background }]}
    >
      <CodexChatBody {...bodyProps} />
    </View>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
    minHeight: 0,
    position: "relative",
  },
});
