import React from "react";
import { StyleSheet, View } from "react-native";
import type { AgentStatus } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  CodexChatHeaderGitDiffButton,
  type CodexChatHeaderGitDiff,
} from "./CodexChatHeaderGitDiffButton";
import { CodexChatHeaderTitle } from "./CodexChatHeaderTitle";
import { ChatHeaderIconButton } from "./ChatHeaderIconButton";

export interface CodexChatHeaderProps {
  status: AgentStatus;
  statusMeta: string;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  gitDiff?: CodexChatHeaderGitDiff | null;
  onSwitchToTerminal(): void;
}

export function CodexChatHeader({
  status,
  statusMeta,
  theme,
  chrome,
  gitDiff,
  onSwitchToTerminal,
}: CodexChatHeaderProps) {
  return (
    <View
      style={[
        styles.header,
        {
          borderBottomColor: chrome.border,
          backgroundColor: theme.background,
        },
      ]}
    >
      <CodexChatHeaderTitle
        status={status}
        statusMeta={statusMeta}
        chrome={chrome}
      />

      {gitDiff ? (
        <CodexChatHeaderGitDiffButton gitDiff={gitDiff} chrome={chrome} />
      ) : null}

      <ChatHeaderIconButton
        icon="terminal-outline"
        accessibilityLabel="Open terminal renderer"
        chrome={chrome}
        onPress={onSwitchToTerminal}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  header: {
    minHeight: 40,
    borderBottomWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    paddingVertical: 5,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
});
