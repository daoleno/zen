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
  conversationScopeKey?: string;
  agentInfo?: CodexChatAgentInfo;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  screenFocused: boolean;
  placeholder?: string;
  minimalComposer?: boolean;
  showAttachmentControl?: boolean;
  keyboardVerticalOffset?: number;
  showUnavailableAction?: boolean;
  emptyTitle?: string;
  emptyBody?: string;
  onSwitchToTerminal?: () => void;
}

function CodexChatSurfaceImpl({
  visible,
  serverId,
  agentId,
  conversationScopeKey,
  agentInfo,
  connectionState,
  connectionIssue,
  theme,
  chrome,
  screenFocused,
  placeholder,
  minimalComposer,
  showAttachmentControl,
  keyboardVerticalOffset,
  showUnavailableAction,
  emptyTitle,
  emptyBody,
  onSwitchToTerminal,
}: CodexChatSurfaceProps) {
  const { bodyProps } = useCodexChatSurfaceState({
    visible,
    serverId,
    agentId,
    conversationScopeKey,
    agentInfo,
    connectionState,
    connectionIssue,
    theme,
    chrome,
    screenFocused,
    placeholder,
    minimalComposer,
    showAttachmentControl,
    keyboardVerticalOffset,
    showUnavailableAction,
    emptyTitle,
    emptyBody,
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

export const CodexChatSurface = React.memo(CodexChatSurfaceImpl);

const styles = StyleSheet.create({
  root: {
    flex: 1,
    minHeight: 0,
    position: "relative",
  },
});
