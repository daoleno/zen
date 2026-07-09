import React from "react";
import { StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { mixHex, relativeLuminance } from "../../theme/colorUtils";

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
  const canvas = chrome.appBackground;
  const pill =
    canvas.startsWith("#") && chrome.surface.startsWith("#")
      ? mixHex(canvas, chrome.surface, 0.55)
      : chrome.surface;
  const labelColor =
    canvas.startsWith("#") && relativeLuminance(canvas) > 0.55
      ? "rgba(0,0,0,0.45)"
      : "rgba(255,255,255,0.55)";

  return (
    <View style={styles.row}>
      <View style={[styles.pill, { backgroundColor: pill }]}>
        <Text style={[styles.label, { color: labelColor }]}>{label}</Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    alignItems: "center",
    marginVertical: 10,
  },
  pill: {
    borderRadius: 999,
    paddingHorizontal: 11,
    paddingVertical: 4,
  },
  label: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 11,
    lineHeight: 14,
    letterSpacing: 0.2,
  },
});
