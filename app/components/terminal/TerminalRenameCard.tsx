import React from "react";
import {
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from "react-native";
import { TypeScale } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";

interface TerminalRenameCardProps {
  draft: string;
  placeholder: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onDraftChange(value: string): void;
  onClose(): void;
  onSave(): void;
}

export function TerminalRenameCard({
  draft,
  placeholder,
  chrome,
  onDraftChange,
  onClose,
  onSave,
}: TerminalRenameCardProps) {
  return (
    <View
      style={[
        styles.renameCard,
        {
          backgroundColor: chrome.surface,
          borderColor: chrome.border,
        },
      ]}
    >
      <Text style={[styles.renameTitle, { color: chrome.text }]}>
        Rename Terminal
      </Text>
      <Text style={[styles.renameHint, { color: chrome.textMuted }]}>
        Only changes the local display name on this device.
      </Text>
      <TextInput
        style={[
          styles.renameInput,
          {
            color: chrome.text,
            borderColor: chrome.border,
            backgroundColor: chrome.surfaceMuted,
          },
        ]}
        value={draft}
        onChangeText={onDraftChange}
        placeholder={placeholder}
        placeholderTextColor={chrome.textSubtle}
        autoFocus
        autoCorrect={false}
        autoCapitalize="none"
        returnKeyType="done"
        onSubmitEditing={onSave}
      />
      <View style={styles.renameActions}>
        <TouchableOpacity
          style={[
            styles.renameButton,
            {
              backgroundColor: chrome.surfaceMuted,
              borderColor: chrome.border,
            },
          ]}
          onPress={onClose}
          activeOpacity={0.84}
        >
          <Text style={[styles.renameButtonText, { color: chrome.textMuted }]}>
            Cancel
          </Text>
        </TouchableOpacity>
        <TouchableOpacity
          style={[
            styles.renameButton,
            {
              backgroundColor: chrome.accent,
              borderColor: chrome.accent,
            },
          ]}
          onPress={onSave}
          activeOpacity={0.84}
        >
          <Text
            style={[
              styles.renameButtonText,
              { color: chrome.textOnAccent },
            ]}
          >
            Save
          </Text>
        </TouchableOpacity>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  renameCard: {
    borderRadius: 18,
    padding: 16,
    borderWidth: 1,
  },
  renameTitle: {
    ...TypeScale.heading,
  },
  renameHint: {
    ...TypeScale.caption,
    marginTop: 4,
  },
  renameInput: {
    marginTop: 14,
    borderRadius: 12,
    borderWidth: 1,
    paddingHorizontal: 12,
    paddingVertical: 11,
    ...TypeScale.compact,
  },
  renameActions: {
    flexDirection: "row",
    justifyContent: "flex-end",
    marginTop: 14,
    gap: 10,
  },
  renameButton: {
    minWidth: 72,
    height: 44,
    borderRadius: 10,
    alignItems: "center",
    justifyContent: "center",
  },
  renameButtonText: {
    ...TypeScale.label,
  },
});
