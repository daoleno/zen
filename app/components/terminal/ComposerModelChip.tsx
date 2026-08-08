import { Ionicons } from "@expo/vector-icons";
import React from "react";
import { StyleSheet, Text, TouchableOpacity } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import {
  COMPOSER_MAX_FONT_SCALE,
  COMPOSER_MODEL_CHIP_HEIGHT,
  COMPOSER_MODEL_CHIP_LABEL_LINE_HEIGHT,
  COMPOSER_MODEL_CHIP_RADIUS,
} from "./composerExpansionMetrics";

interface ComposerModelChipProps {
  label: string;
  accessibilityLabel: string;
  chrome: TerminalThemeChrome;
  onPress(): void;
}

/**
 * Concise active-model control for the expanded Composer action row. Only the
 * host may decide whether this renders; the chip itself never fabricates a
 * label or a mutation. The touch target fills the 44 pt action slot; the
 * full label stays available through accessibilityLabel while the visible
 * text truncates at one line within the capped font scale.
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
        {
          height: COMPOSER_MODEL_CHIP_HEIGHT,
          borderRadius: COMPOSER_MODEL_CHIP_RADIUS,
          backgroundColor: chrome.surfaceMuted,
          borderColor: chrome.border,
        },
      ]}
      onPress={onPress}
      activeOpacity={0.7}
    >
      <Ionicons name="sparkles-outline" size={14} color={chrome.accent} />
      <Text
        numberOfLines={1}
        ellipsizeMode="tail"
        maxFontSizeMultiplier={COMPOSER_MAX_FONT_SCALE}
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
    maxWidth: "100%",
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
    paddingHorizontal: 10,
  },
  label: {
    fontFamily: Typography.chatFont,
    fontSize: 13,
    lineHeight: COMPOSER_MODEL_CHIP_LABEL_LINE_HEIGHT,
    flexShrink: 1,
  },
});
