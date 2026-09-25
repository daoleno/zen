import React from "react";
import { StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { ContinuousCorners, TypeScale } from "../../constants/tokens";
import { mixHex, relativeLuminance } from "../../theme/colorUtils";

export type ZenDateDividerItem = {
  type: "date-divider";
  id: string;
  label: string;
};

export function InterfaceTimelineDateDivider({
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
      <View
        style={[
          styles.pill,
          { backgroundColor: pill, borderColor: chrome.border },
        ]}
      >
        <Text
          numberOfLines={1}
          maxFontSizeMultiplier={1.6}
          style={[styles.label, { color: labelColor }]}
        >
          {label}
        </Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  // Glass pill: canvas-mixed fill, hairline stroke, generous air above and
  // below so a day boundary reads as a pause in the conversation.
  row: {
    alignItems: "center",
    marginTop: 14,
    marginBottom: 12,
  },
  pill: {
    borderRadius: 999,
    ...ContinuousCorners,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    paddingVertical: 4,
  },
  label: {
    ...TypeScale.micro,
    letterSpacing: 0.2,
  },
});
