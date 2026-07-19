import React from "react";
import {
  StyleSheet,
  TouchableOpacity,
  View,
  type StyleProp,
  type ViewStyle,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

interface InterfaceComposerAttachmentRemoveButtonProps {
  attachmentName: string;
  chrome: TerminalThemeChrome;
  onPress(): void;
  style?: StyleProp<ViewStyle>;
}

export function InterfaceComposerAttachmentRemoveButton({
  attachmentName,
  chrome,
  onPress,
  style,
}: InterfaceComposerAttachmentRemoveButtonProps) {
  return (
    <TouchableOpacity
      accessibilityLabel={`Remove ${attachmentName}`}
      accessibilityRole="button"
      style={[styles.remove, style]}
      onPress={onPress}
      activeOpacity={0.72}
    >
      <View style={[styles.badge, { backgroundColor: chrome.appBackground }]}>
        <Ionicons name="close" size={12} color={chrome.text} />
      </View>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  remove: {
    width: 44,
    height: 44,
    alignItems: "center",
    justifyContent: "center",
  },
  badge: {
    width: 20,
    height: 20,
    borderRadius: 10,
    alignItems: "center",
    justifyContent: "center",
  },
});
