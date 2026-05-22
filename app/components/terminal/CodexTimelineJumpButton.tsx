import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";

interface CodexTimelineJumpButtonProps {
  bottom: number;
  chrome: TerminalThemeChrome;
  onPress(): void;
}

export function CodexTimelineJumpButton({
  bottom,
  chrome,
  onPress,
}: CodexTimelineJumpButtonProps) {
  return (
    <TouchableOpacity
      accessibilityLabel="Jump to latest"
      accessibilityRole="button"
      style={[
        styles.jumpButton,
        {
          backgroundColor: chrome.surfaceMuted,
          borderColor: chrome.borderStrong,
          bottom,
        },
      ]}
      onPress={onPress}
      activeOpacity={0.82}
    >
      <Ionicons name="arrow-down" size={15} color={chrome.accent} />
      <Text style={[styles.jumpButtonText, { color: chrome.textMuted }]}>
        Latest
      </Text>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  jumpButton: {
    position: "absolute",
    right: 14,
    minHeight: 32,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 10,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
    zIndex: 4,
  },
  jumpButtonText: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
});
