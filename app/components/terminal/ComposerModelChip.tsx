import { Ionicons } from "@expo/vector-icons";
import React from "react";
import { StyleSheet, Text, TouchableOpacity } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";

interface ComposerModelChipProps {
  label: string;
  accessibilityLabel: string;
  chrome: TerminalThemeChrome;
  onPress(): void;
}

/**
 * Concise active-model control for the expanded Composer action row. Only the
 * host may decide whether this renders; the chip itself never fabricates a
 * label or a mutation.
 */
export function ComposerModelChip({
  label,
  accessibilityLabel,
  chrome,
  onPress,
}: ComposerModelChipProps) {
  return (
    <TouchableOpacity
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
      accessibilityHint="Opens the Session model selection"
      style={[
        styles.chip,
        { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
      ]}
      onPress={onPress}
      activeOpacity={0.7}
    >
      <Ionicons name="sparkles-outline" size={14} color={chrome.accent} />
      <Text
        numberOfLines={1}
        style={[styles.label, { color: chrome.textMuted }]}
      >
        {label}
      </Text>
      <Ionicons name="chevron-down" size={12} color={chrome.textSubtle} />
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  chip: {
    height: 36,
    maxWidth: "100%",
    borderRadius: 18,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
    paddingHorizontal: 10,
  },
  label: {
    fontFamily: Typography.chatFont,
    fontSize: 13,
    lineHeight: 18,
    flexShrink: 1,
  },
});
