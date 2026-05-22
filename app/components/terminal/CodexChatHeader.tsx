import React from "react";
import { StyleSheet, Text, View } from "react-native";
import {
  Typography,
  statusColor,
  type AgentStatus,
} from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { ChatHeaderIconButton } from "./CodexChatControls";

interface CodexChatHeaderProps {
  status: AgentStatus;
  statusMeta: string;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  gitDiff?: {
    tone: "clean" | "dirty" | "error" | "loading";
    onPress(): void;
  } | null;
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
      <View style={styles.titleGroup}>
        <View style={styles.titleRow}>
          <Text style={[styles.title, { color: chrome.text }]} numberOfLines={1}>
            Codex
          </Text>
          <View
            style={[
              styles.statusDot,
              { backgroundColor: statusColor(status) },
            ]}
          />
        </View>
        <Text style={[styles.meta, { color: chrome.textSubtle }]} numberOfLines={1}>
          {statusMeta}
        </Text>
      </View>

      {gitDiff ? (
        <ChatHeaderIconButton
          icon={gitDiff.tone === "loading" ? "sync-outline" : "git-branch-outline"}
          accessibilityLabel="Git diff"
          chrome={chrome}
          color={gitDiff.tone === "dirty" ? chrome.accent : chrome.textMuted}
          onPress={gitDiff.onPress}
        />
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
  titleGroup: {
    flex: 1,
    minWidth: 0,
  },
  titleRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
  },
  title: {
    fontSize: 14,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
  },
  meta: {
    marginTop: 1,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFont,
  },
  statusDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
});
