import React from "react";
import {
  StyleSheet,
  Text,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { CodexSlashCommand } from "../../services/websocket";

export interface CodexQuickCommandRoutePresentation {
  label: string;
  color: string;
}

interface CodexQuickCommandRouteBadgeProps {
  route: CodexQuickCommandRoutePresentation;
}

export function CodexQuickCommandRouteBadge({
  route,
}: CodexQuickCommandRouteBadgeProps) {
  return (
    <View style={[styles.badge, { borderColor: route.color }]}>
      <Text
        style={[styles.badgeText, { color: route.color }]}
        numberOfLines={1}
      >
        {route.label}
      </Text>
    </View>
  );
}

export function getCodexQuickCommandRoutePresentation(
  command: CodexSlashCommand,
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
): CodexQuickCommandRoutePresentation {
  if (
    command.execution === "unsupported" ||
    (!command.terminal_supported && !command.chat_supported)
  ) {
    return {
      label: "Unsupported",
      color: theme.red,
    };
  }
  if (
    command.execution === "chat-native" ||
    command.execution === "timeline-output"
  ) {
    if (command.output.kind === "diff") {
      return {
        label: "Diff",
        color: theme.green,
      };
    }
    if (command.output.kind === "status-card") {
      return {
        label: "Status",
        color: theme.green,
      };
    }
    return {
      label: "Chat",
      color: theme.green,
    };
  }
  if (
    command.interactive ||
    command.input.kind === "picker" ||
    command.input.kind === "form"
  ) {
    return {
      label: "Interactive",
      color: theme.yellow,
    };
  }
  if (command.execution === "insert-only") {
    return {
      label: "Insert",
      color: theme.cyan,
    };
  }
  return {
    label: "Terminal",
    color: chrome.textSubtle,
  };
}

const styles = StyleSheet.create({
  badge: {
    maxWidth: 86,
    minHeight: 22,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 7,
    alignItems: "center",
    justifyContent: "center",
  },
  badgeText: {
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFontMedium,
  },
});
