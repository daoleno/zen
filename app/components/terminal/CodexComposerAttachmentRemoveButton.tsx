import React from "react";
import {
  StyleSheet,
  TouchableOpacity,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

interface CodexComposerAttachmentRemoveButtonProps {
  attachmentName: string;
  chrome: TerminalThemeChrome;
  onPress(): void;
}

export function CodexComposerAttachmentRemoveButton({
  attachmentName,
  chrome,
  onPress,
}: CodexComposerAttachmentRemoveButtonProps) {
  return (
    <TouchableOpacity
      accessibilityLabel={`Remove ${attachmentName}`}
      accessibilityRole="button"
      style={styles.remove}
      onPress={onPress}
      activeOpacity={0.72}
    >
      <Ionicons name="close" size={13} color={chrome.textSubtle} />
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  remove: {
    width: 24,
    height: 28,
    alignItems: "center",
    justifyContent: "center",
  },
});
