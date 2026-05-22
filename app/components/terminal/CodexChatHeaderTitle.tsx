import React from "react";
import {
  StyleSheet,
  Text,
  View,
} from "react-native";
import {
  Typography,
  statusColor,
  type AgentStatus,
} from "../../constants/tokens";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

interface CodexChatHeaderTitleProps {
  status: AgentStatus;
  statusMeta: string;
  chrome: TerminalThemeChrome;
}

export function CodexChatHeaderTitle({
  status,
  statusMeta,
  chrome,
}: CodexChatHeaderTitleProps) {
  return (
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
  );
}

const styles = StyleSheet.create({
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
