import React from "react";
import {
  KeyboardAvoidingView,
  Modal,
  Platform,
  StyleSheet,
  TouchableOpacity,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { TerminalRenameCard } from "./TerminalRenameCard";

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

        <TerminalRenameCard
          draft={draft}
          placeholder={placeholder}
          chrome={chrome}
          theme={theme}
          onDraftChange={onDraftChange}
          onClose={onClose}
          onSave={onSave}
        />
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
});
