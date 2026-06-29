import React from "react";
import { StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";

export type ZenDateDividerItem = {
  type: "date-divider";
  id: string;
  label: string;
};

export function CodexTimelineDateDivider({
  label,
  chrome,
}: {
  label: string;
  chrome: TerminalThemeChrome;
}) {
  return (
    <View style={styles.row}>
      <View
        style={[
          styles.pill,
          {
            backgroundColor: chrome.surface,
            borderColor: chrome.border,
          },
        ]}
      >
        <Text style={[styles.label, { color: chrome.textMuted }]}>{label}</Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    alignItems: "center",
    marginVertical: 14,
  },
  pill: {
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    paddingVertical: 4,
  },
  label: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 12,
    lineHeight: 16,
  },
});