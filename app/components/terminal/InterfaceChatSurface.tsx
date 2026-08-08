import React from "react";
import { StyleSheet, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { isAmbientChatChrome } from "../../constants/themedSurfaces";
import type { ConnectionState } from "../../store/agents";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { ComposerModelControlPresentation } from "../../services/providers/sessionModelHelpers";
import { InterfaceChatBody } from "./InterfaceChatBody";
import { useInterfaceChatSurfaceState } from "./useInterfaceChatSurfaceState";
import type { InterfaceChatAgentInfo } from "./InterfaceChatSession";

interface InterfaceChatSurfaceProps {
  visible: boolean;
  serverId: string;
  serverUrl: string;
  daemonId: string;
  agentId: string;
  conversationScopeKey?: string;
  agentInfo?: InterfaceChatAgentInfo;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  screenFocused: boolean;
  initialComposerFocusGrant?: string | null;
  readOnly?: boolean;
  placeholder?: string;
  keyboardVerticalOffset?: number;
  topChromeInset?: number;
  showUnavailableAction?: boolean;
  emptyTitle?: string;
  emptyBody?: string;
  onBrainWorkEventActivate?: (
    event: import("../brain/brainWorkEvent").BrainWorkResultEvent,
    canOpenSession: boolean,
  ) => void;
  openSessionIds?: ReadonlySet<string>;
  composerAccessory?: React.ReactNode;
  onDraftChange?: (value: string) => void;
  renderComposerAccessory?: (args: {
    draft: string;
    setDraft: (value: string) => void;
  }) => React.ReactNode;
  composerModelControl?: ComposerModelControlPresentation | null;
  onComposerModelControlPress?: () => void;
  onSwitchToTerminal?: () => void;
  onConsumeInitialComposerFocus?: () => void;
}

function InterfaceChatSurfaceImpl({
  visible,
  serverId,
  serverUrl,
  daemonId,
  agentId,
  conversationScopeKey,
  agentInfo,
  connectionState,
  connectionIssue,
  theme,
  chrome,
  screenFocused,
  initialComposerFocusGrant,
  readOnly = false,
  placeholder,
  keyboardVerticalOffset,
  topChromeInset,
  showUnavailableAction,
  emptyTitle,
  emptyBody,
  onBrainWorkEventActivate,
  openSessionIds,
  composerAccessory,
  onDraftChange,
  renderComposerAccessory,
  composerModelControl,
  onComposerModelControlPress,
  onSwitchToTerminal,
  onConsumeInitialComposerFocus,
}: InterfaceChatSurfaceProps) {
  const { bodyProps } = useInterfaceChatSurfaceState({
    visible,
    serverId,
    serverUrl,
    daemonId,
    agentId,
    conversationScopeKey,
    agentInfo,
    connectionState,
    connectionIssue,
    theme,
    chrome,
    screenFocused,
    initialComposerFocusGrant,
    placeholder,
    keyboardVerticalOffset,
    topChromeInset,
    showUnavailableAction,
    emptyTitle,
    emptyBody,
    onBrainWorkEventActivate,
    openSessionIds,
    composerAccessory,
    onDraftChange,
    renderComposerAccessory,
    composerModelControl,
    onComposerModelControlPress,
    onSwitchToTerminal,
    onConsumeInitialComposerFocus,
  });

  const canvasBackground = isAmbientChatChrome(chrome)
    ? "transparent"
    : theme.background;

  return (
    <View style={[styles.root, { backgroundColor: canvasBackground }]}>
      <InterfaceChatBody {...bodyProps} readOnly={readOnly} />
    </View>
  );
}

export const InterfaceChatSurface = React.memo(InterfaceChatSurfaceImpl);

const styles = StyleSheet.create({
  root: {
    flex: 1,
    minHeight: 0,
    position: "relative",
  },
});
