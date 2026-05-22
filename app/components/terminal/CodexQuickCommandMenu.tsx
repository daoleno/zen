import React from "react";
import {
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { CodexSlashCommand } from "../../services/websocket";
import {
  slashCommandIcon,
  slashCommandTitle,
} from "./codexSlashCommandPresentation";

interface CodexQuickCommandMenuProps {
  commands: CodexSlashCommand[];
  commandQuery: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onSelectCommand(command: CodexSlashCommand): void;
}

export function CodexQuickCommandMenu({
  commands,
  commandQuery,
  chrome,
  theme,
  onSelectCommand,
}: CodexQuickCommandMenuProps) {
  return (
    <View
      style={[
        styles.menu,
        { backgroundColor: chrome.surface, borderColor: chrome.border },
      ]}
    >
      {commands.length > 0 ? (
        <ScrollView
          style={styles.scroller}
          keyboardShouldPersistTaps="handled"
          showsVerticalScrollIndicator={commands.length > 5}
        >
          {commands.map((command) => {
            const selected = commandQuery === command.value;
            const routeColor = slashCommandRouteColor(command, chrome, theme);
            return (
              <TouchableOpacity
                key={command.value}
                accessibilityLabel={`${slashCommandRouteLabel(command)} ${command.value}`}
                style={[
                  styles.row,
                  selected ? { backgroundColor: chrome.surfaceMuted } : null,
                ]}
                onPress={() => onSelectCommand(command)}
                activeOpacity={0.78}
              >
                <View
                  style={[
                    styles.icon,
                    { backgroundColor: chrome.surfaceMuted },
                  ]}
                >
                  <Ionicons
                    name={slashCommandIcon(command.name)}
                    size={15}
                    color={chrome.accent}
                  />
                </View>
                <View style={styles.copy}>
                  <Text
                    style={[styles.title, { color: chrome.text }]}
                    numberOfLines={1}
                  >
                    {command.title || slashCommandTitle(command.name)}
                  </Text>
                  <Text
                    style={[styles.description, { color: chrome.textSubtle }]}
                    numberOfLines={1}
                  >
                    {command.description}
                  </Text>
                </View>
                <View style={[styles.badge, { borderColor: routeColor }]}>
                  <Text
                    style={[styles.badgeText, { color: routeColor }]}
                    numberOfLines={1}
                  >
                    {slashCommandRouteLabel(command)}
                  </Text>
                </View>
                <Text
                  style={[styles.value, { color: chrome.textMuted }]}
                  numberOfLines={1}
                >
                  {command.value}
                </Text>
                <Ionicons
                  name="chevron-forward"
                  size={14}
                  color={chrome.textSubtle}
                />
              </TouchableOpacity>
            );
          })}
        </ScrollView>
      ) : (
        <View style={styles.empty}>
          <Ionicons name="search-outline" size={15} color={chrome.textSubtle} />
          <Text style={[styles.description, { color: chrome.textSubtle }]}>
            No matching command
          </Text>
        </View>
      )}
    </View>
  );
}

function slashCommandRouteLabel(command: CodexSlashCommand) {
  if (command.execution === "unsupported" || !command.terminal_supported && !command.chat_supported) {
    return "Unsupported";
  }
  if (command.execution === "chat-native" || command.execution === "timeline-output") {
    if (command.output.kind === "diff") {
      return "Diff";
    }
    if (command.output.kind === "status-card") {
      return "Status";
    }
    return "Chat";
  }
  if (command.interactive || command.input.kind === "picker" || command.input.kind === "form") {
    return "Interactive";
  }
  if (command.execution === "insert-only") {
    return "Insert";
  }
  return "Terminal";
}

function slashCommandRouteColor(
  command: CodexSlashCommand,
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
) {
  if (command.execution === "unsupported" || !command.terminal_supported && !command.chat_supported) {
    return theme.red;
  }
  if (command.execution === "chat-native" || command.execution === "timeline-output") {
    return theme.green;
  }
  if (command.interactive || command.input.kind === "picker" || command.input.kind === "form") {
    return theme.yellow;
  }
  if (command.execution === "insert-only") {
    return theme.cyan;
  }
  return chrome.textSubtle;
}

const styles = StyleSheet.create({
  menu: {
    marginBottom: 8,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  scroller: {
    maxHeight: 330,
  },
  row: {
    minHeight: 50,
    flexDirection: "row",
    alignItems: "center",
    gap: 9,
    paddingHorizontal: 10,
    paddingVertical: 7,
  },
  icon: {
    width: 28,
    height: 28,
    borderRadius: 8,
    alignItems: "center",
    justifyContent: "center",
  },
  copy: {
    flex: 1,
    minWidth: 0,
  },
  title: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.uiFontMedium,
  },
  description: {
    marginTop: 1,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
  },
  value: {
    maxWidth: 72,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
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
  empty: {
    minHeight: 44,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 12,
  },
});
