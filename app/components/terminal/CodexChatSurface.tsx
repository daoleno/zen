import React from "react";
import { StyleSheet, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { isAmbientChatChrome } from "../../constants/themedSurfaces";
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
  readOnly?: boolean;
  placeholder?: string;
  keyboardVerticalOffset?: number;
  topChromeInset?: number;
  showUnavailableAction?: boolean;
  emptyTitle?: string;
  emptyBody?: string;
  composerAccessory?: React.ReactNode;
  onDraftChange?: (value: string) => void;
  renderComposerAccessory?: (args: {
    draft: string;
    setDraft: (value: string) => void;
  }) => React.ReactNode;
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
  readOnly = false,
  placeholder,
  keyboardVerticalOffset,
  topChromeInset,
  showUnavailableAction,
  emptyTitle,
  emptyBody,
  composerAccessory,
  onDraftChange,
  renderComposerAccessory,
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
    keyboardVerticalOffset,
    topChromeInset,
    showUnavailableAction,
    emptyTitle,
    emptyBody,
    composerAccessory,
    onDraftChange,
    renderComposerAccessory,
    onSwitchToTerminal,
  });

  const canvasBackground = isAmbientChatChrome(chrome)
    ? "transparent"
    : theme.background;

  return (
    <View style={[styles.root, { backgroundColor: canvasBackground }]}>
      <CodexChatBody {...bodyProps} readOnly={readOnly} />
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
