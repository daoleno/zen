import React from "react";
import {
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";

export function ZenPlanHeader({
  accentColor,
  chrome,
}: {
  accentColor: string;
  chrome: TerminalThemeChrome;
}) {
  return (
    <View style={styles.row}>
      <Ionicons name="checkbox-outline" size={13} color={accentColor} />
      <Text
        style={[styles.title, { color: chrome.textSubtle }]}
        numberOfLines={1}
      >
        Updated Plan
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    alignSelf: "flex-start",
    minHeight: 24,
    maxWidth: "100%",
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    opacity: 0.78,
  },
  title: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
});
