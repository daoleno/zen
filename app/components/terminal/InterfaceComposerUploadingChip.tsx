import React from "react";
import { StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { ComposerLoadingDots } from "./ComposerLoadingDots";

interface InterfaceComposerUploadingChipProps {
  chrome: TerminalThemeChrome;
}

export function InterfaceComposerUploadingChip({
  chrome,
}: InterfaceComposerUploadingChipProps) {
  return (
    <View
      accessibilityLabel="Uploading attachment"
      style={[
        styles.chip,
        { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
      ]}
    >
      <View style={styles.loader}>
        <ComposerLoadingDots color={chrome.accent} size={8} />
      </View>
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
  loader: {
    width: 18,
    alignItems: "center",
  },
  name: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
});
