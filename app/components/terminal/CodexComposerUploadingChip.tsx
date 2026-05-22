import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  View,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";

interface CodexComposerUploadingChipProps {
  chrome: TerminalThemeChrome;
}

export function CodexComposerUploadingChip({
  chrome,
}: CodexComposerUploadingChipProps) {
  return (
    <View
      accessibilityLabel="Uploading attachment"
      style={[
        styles.chip,
        { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
      ]}
    >
      <ActivityIndicator size="small" color={chrome.accent} />
      <Text style={[styles.name, { color: chrome.textMuted }]}>Uploading</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  chip: {
    maxWidth: 220,
    minHeight: 36,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: 9,
    paddingRight: 10,
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
  },
  name: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
});
