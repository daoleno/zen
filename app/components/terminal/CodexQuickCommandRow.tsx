import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { CodexSlashCommand } from "../../services/websocket";
import { slashCommandIcon } from "./codexSlashCommandPresentation";

interface CodexQuickCommandRowProps {
  command: CodexSlashCommand;
  selected: boolean;
  chrome: TerminalThemeChrome;
  onSelect(command: CodexSlashCommand): void;
}

export function CodexQuickCommandRow({
  command,
  selected,
  chrome,
  onSelect,
}: CodexQuickCommandRowProps) {
  return (
    <TouchableOpacity
      accessibilityLabel={`${command.value} ${command.description}`}
      style={[
        styles.row,
        selected ? { backgroundColor: chrome.surfaceMuted } : null,
      ]}
      onPress={() => onSelect(command)}
      activeOpacity={0.78}
    >
      <View style={[styles.icon, { backgroundColor: chrome.surfaceMuted }]}>
        <Ionicons
          name={slashCommandIcon(command.name)}
          size={15}
          color={chrome.accent}
        />
      </View>
      <View style={styles.copy}>
        <Text
          style={[styles.value, { color: chrome.accent }]}
          numberOfLines={1}
        >
          {command.value}
        </Text>
        <Text
          style={[styles.description, { color: chrome.textSubtle }]}
          numberOfLines={1}
        >
          {command.description}
        </Text>
      </View>
      <Ionicons name="chevron-forward" size={14} color={chrome.textSubtle} />
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
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
  value: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.terminalFont,
  },
  description: {
    marginTop: 1,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
  },
});
