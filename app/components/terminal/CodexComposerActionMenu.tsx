import React from "react";
import {
  ScrollView,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { TouchableOpacity } from "react-native-gesture-handler";
import { Typography } from "../../constants/tokens";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { CodexSlashCommand } from "../../services/websocket";
import { CodexQuickCommandRow } from "./CodexQuickCommandRow";

const COMMAND_GROUPS = [
  { key: "session", label: "Session" },
  { key: "navigation", label: "Navigation" },
  { key: "tools", label: "Project Tools" },
  { key: "settings", label: "Settings" },
  { key: "management", label: "Management" },
] as const;

interface CommandGroup {
  key: string;
  label: string;
  commands: CodexSlashCommand[];
}

interface CodexComposerActionMenuProps {
  visible: boolean;
  showComposerActions: boolean;
  showCommandList: boolean;
  canAttach: boolean;
  uploading: boolean;
  commands: CodexSlashCommand[];
  commandQuery: string;
  chrome: TerminalThemeChrome;
  onUploadPress(): void;
  onSelectCommand(command: CodexSlashCommand): void;
}

export function CodexComposerActionMenu({
  visible,
  showComposerActions,
  showCommandList,
  canAttach,
  uploading,
  commands,
  commandQuery,
  chrome,
  onUploadPress,
  onSelectCommand,
}: CodexComposerActionMenuProps) {
  if (!visible) {
    return null;
  }

  const commandGroups = groupCommands(commands);

  return (
    <View
      style={[
        styles.menu,
        { backgroundColor: chrome.surface, borderColor: chrome.border },
      ]}
    >
      {showComposerActions ? (
        <TouchableOpacity
          accessibilityLabel="Upload file"
          accessibilityRole="button"
          accessibilityState={{ disabled: !canAttach, busy: uploading }}
          style={[
            styles.actionRow,
            !canAttach ? { backgroundColor: chrome.disabledSurface } : null,
          ]}
          onPress={onUploadPress}
          disabled={!canAttach}
          activeOpacity={0.78}
        >
          <View style={[styles.icon, { backgroundColor: chrome.surfaceMuted }]}>
            <Ionicons
              name={
                uploading ? "cloud-upload-outline" : "document-attach-outline"
              }
              size={16}
              color={canAttach ? chrome.accent : chrome.textSubtle}
            />
          </View>
          <View style={styles.actionCopy}>
            <Text
              style={[styles.title, { color: chrome.text }]}
              numberOfLines={1}
            >
              Attach File
            </Text>
            <Text
              style={[styles.description, { color: chrome.textSubtle }]}
              numberOfLines={1}
            >
              {uploading ? "Uploading..." : "Add a file to this message"}
            </Text>
          </View>
          <Ionicons name="chevron-forward" size={14} color={chrome.textSubtle} />
        </TouchableOpacity>
      ) : null}

      {showCommandList ? (
        <View
          style={[
            styles.commandSection,
            showComposerActions ? { borderTopColor: chrome.border } : null,
            !showComposerActions ? styles.commandSectionFirst : null,
          ]}
        >
          <View style={styles.sectionHeader}>
            <Ionicons
              name="code-slash-outline"
              size={14}
              color={chrome.textSubtle}
            />
            <Text style={[styles.sectionLabel, { color: chrome.textSubtle }]}>
              Commands
            </Text>
          </View>
          {commands.length > 0 ? (
            <ScrollView
              style={styles.scroller}
              keyboardShouldPersistTaps="handled"
              showsVerticalScrollIndicator={commands.length > 6}
            >
              {commandGroups.map((group, groupIndex) => {
                return (
                  <View key={group.key}>
                    <Text
                      style={[
                        styles.commandGroupLabel,
                        groupIndex === 0 ? styles.commandGroupLabelFirst : null,
                        { color: chrome.textMuted },
                      ]}
                      numberOfLines={1}
                    >
                      {group.label}
                    </Text>
                    {group.commands.map((command) => {
                      const selected = commandQuery === command.value;
                      return (
                        <CodexQuickCommandRow
                          key={command.value}
                          command={command}
                          selected={selected}
                          chrome={chrome}
                          onSelect={onSelectCommand}
                        />
                      );
                    })}
                  </View>
                );
              })}
              <View style={styles.scrollerBottomSpacer} />
            </ScrollView>
          ) : (
            <View style={styles.empty}>
              <Ionicons
                name="search-outline"
                size={15}
                color={chrome.textSubtle}
              />
              <Text style={[styles.description, { color: chrome.textSubtle }]}>
                No matching command
              </Text>
            </View>
          )}
        </View>
      ) : null}
    </View>
  );
}

function groupCommands(commands: CodexSlashCommand[]): CommandGroup[] {
  const grouped: CommandGroup[] = COMMAND_GROUPS.map((group) => ({
    ...group,
    commands: commands.filter((command) => command.category === group.key),
  }));
  const knownCategories = new Set(COMMAND_GROUPS.map((group) => group.key));
  const otherCommands = commands.filter(
    (command) => !knownCategories.has(command.category as any),
  );
  if (otherCommands.length > 0) {
    grouped.push({
      key: "other",
      label: "Other",
      commands: otherCommands,
    });
  }
  return grouped.filter((group) => group.commands.length > 0);
}

const styles = StyleSheet.create({
  menu: {
    borderRadius: 14,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  actionRow: {
    minHeight: 52,
    flexDirection: "row",
    alignItems: "center",
    gap: 9,
    paddingHorizontal: 10,
    paddingVertical: 8,
  },
  icon: {
    width: 28,
    height: 28,
    borderRadius: 8,
    alignItems: "center",
    justifyContent: "center",
  },
  actionCopy: {
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
  commandSection: {
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  commandSectionFirst: {
    borderTopWidth: 0,
  },
  sectionHeader: {
    minHeight: 30,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    paddingHorizontal: 11,
    paddingTop: 6,
    paddingBottom: 4,
  },
  sectionLabel: {
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.uiFontMedium,
  },
  scroller: {
    maxHeight: 320,
  },
  scrollerBottomSpacer: {
    height: 6,
  },
  commandGroupLabel: {
    paddingHorizontal: 11,
    paddingTop: 10,
    paddingBottom: 2,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
  commandGroupLabelFirst: {
    paddingTop: 2,
  },
  empty: {
    minHeight: 44,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 12,
  },
});
