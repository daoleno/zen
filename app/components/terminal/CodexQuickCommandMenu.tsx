import React from "react";
import {
  ScrollView,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { CodexSlashCommand } from "../../services/websocket";
import { CodexQuickCommandRow } from "./CodexQuickCommandRow";

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
            return (
              <CodexQuickCommandRow
                key={command.value}
                command={command}
                selected={selected}
                chrome={chrome}
                theme={theme}
                onSelect={onSelectCommand}
              />
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
  description: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
  },
  empty: {
    minHeight: 44,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 12,
  },
});
