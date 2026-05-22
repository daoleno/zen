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
import { CodexChatHeader } from "./CodexChatHeader";
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
  gitDiff?: {
    label: string;
    tone: "clean" | "dirty" | "error" | "loading";
    onPress(): void;
  } | null;
  onSwitchToTerminal(): void;
  onOpenGitDiff?: () => void;
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
  gitDiff,
  onSwitchToTerminal,
  onOpenGitDiff,
}: CodexChatSurfaceProps) {
  const { headerProps, bodyProps } = useCodexChatSurfaceState({
    serverId,
    agentId,
    agent,
    connectionState,
    connectionIssue,
    theme,
    chrome,
    screenFocused,
    gitDiff,
    onSwitchToTerminal,
    onOpenGitDiff,
  });

  return (
    <View
      style={[styles.root, { backgroundColor: theme.background }]}
    >
      <CodexChatHeader {...headerProps} />

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
