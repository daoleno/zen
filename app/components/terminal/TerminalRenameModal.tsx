import React from "react";
import {
  KeyboardAvoidingView,
  Modal,
  Platform,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from "react-native";
import { Colors, Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";

interface TerminalRenameModalProps {
  visible: boolean;
  draft: string;
  placeholder: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onDraftChange(value: string): void;
  onClose(): void;
  onSave(): void;
}

export function TerminalRenameModal({
  visible,
  draft,
  placeholder,
  chrome,
  theme,
  onDraftChange,
  onClose,
  onSave,
}: TerminalRenameModalProps) {
  return (
    <Modal
      visible={visible}
      transparent
      animationType="fade"
      onRequestClose={onClose}
    >
      <KeyboardAvoidingView
        style={styles.renameRoot}
        behavior={Platform.OS === "ios" ? "padding" : "height"}
      >
        <TouchableOpacity
          style={styles.modalBackdrop}
          activeOpacity={1}
          onPress={onClose}
        />

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
                styles.renameButtonPrimary,
                {
                  backgroundColor: chrome.accent,
                  borderColor: chrome.borderStrong,
                },
              ]}
              onPress={onSave}
              activeOpacity={0.84}
            >
              <Text
                style={[
                  styles.renameButtonText,
                  styles.renameButtonTextPrimary,
                  { color: theme.background },
                ]}
              >
                Save
              </Text>
            </TouchableOpacity>
          </View>
        </View>
      </KeyboardAvoidingView>
    </Modal>
  );
}

const styles = StyleSheet.create({
  renameRoot: {
    flex: 1,
    justifyContent: "center",
    paddingHorizontal: 20,
  },
  modalBackdrop: {
    ...StyleSheet.absoluteFill,
    backgroundColor: "rgba(6, 8, 12, 0.58)",
  },
  renameCard: {
    borderRadius: 18,
    padding: 16,
    backgroundColor: "#161F2B",
    borderWidth: 1,
    borderColor: "rgba(255,255,255,0.06)",
  },
  renameTitle: {
    color: Colors.textPrimary,
    fontSize: 17,
    fontFamily: Typography.uiFontMedium,
  },
  renameHint: {
    marginTop: 4,
    color: "#7D8CA0",
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  renameInput: {
    marginTop: 14,
    borderRadius: 12,
    borderWidth: 1,
    borderColor: "#263345",
    backgroundColor: "#111923",
    color: Colors.textPrimary,
    paddingHorizontal: 12,
    paddingVertical: 11,
    fontSize: 14,
    fontFamily: Typography.uiFont,
  },
  renameActions: {
    flexDirection: "row",
    justifyContent: "flex-end",
    marginTop: 14,
    gap: 10,
  },
  renameButton: {
    minWidth: 72,
    height: 38,
    borderRadius: 10,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "#202A38",
  },
  renameButtonPrimary: {
    backgroundColor: Colors.accent,
  },
  renameButtonText: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  renameButtonTextPrimary: {
    color: "#07111E",
  },
});
