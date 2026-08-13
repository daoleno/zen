import React from "react";
import { StyleSheet, Text, TouchableOpacity } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import {
  COMPOSER_MAX_FONT_SCALE,
  COMPOSER_MODEL_CONTROL_HEIGHT,
  COMPOSER_MODEL_CONTROL_LABEL_LINE_HEIGHT,
} from "./composerExpansionMetrics";

interface ComposerModelChipProps {
  label: string;
  accessibilityLabel: string;
  chrome: TerminalThemeChrome;
  /** Opens the Session model sheet; the native sheet positions itself. */
  onPress(): void;
}

/**
 * Quiet current-model control for the expanded Composer action row, placed
 * immediately left of Send/Stop. Text-only: no border, no icon, no chrome —
 * the model name is the whole control. Only the host may decide whether this
 * renders; the control never fabricates a label or a mutation. The touch
 * target fills the 44 pt action slot; the full label stays available through
 * accessibilityLabel while the visible text truncates at one line within the
 * capped font scale. Pressing opens the model sheet — the sheet is a native
 * bottom sheet and needs no measured anchor.
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
      style={[styles.control, { height: COMPOSER_MODEL_CONTROL_HEIGHT }]}
      onPress={onPress}
      activeOpacity={0.7}
    >
      <Text
        numberOfLines={1}
        ellipsizeMode="tail"
        maxFontSizeMultiplier={COMPOSER_MAX_FONT_SCALE}
        style={[styles.label, { color: chrome.textMuted }]}
      >
        {label}
      </Text>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  control: {
    maxWidth: "100%",
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "flex-end",
  },
  label: {
    fontFamily: Typography.chatFont,
    fontSize: 13,
    lineHeight: COMPOSER_MODEL_CONTROL_LABEL_LINE_HEIGHT,
    flexShrink: 1,
  },
});
