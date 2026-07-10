import React from "react";
import {
  StyleSheet,
  View,
} from "react-native";
import type {
  StyleProp,
  ViewStyle,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

interface CodexTimelineExpandedBlockProps {
  chrome: TerminalThemeChrome;
  children: React.ReactNode;
  style?: StyleProp<ViewStyle>;
  borderColor?: string;
}
export function CodexTimelineExpandedBlock({
  chrome,
  borderColor,
  children,
  style,
}: CodexTimelineExpandedBlockProps) {
  return (
    <View
      style={[
        styles.expanded,
        style,
        {
          borderColor: borderColor ?? chrome.border,
        },
      ]}
    >
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  expanded: {
    marginTop: 2,
    marginLeft: 18,
    maxWidth: "94%",
    borderLeftWidth: 2,
    paddingLeft: 10,
    paddingRight: 4,
    paddingTop: 4,
    paddingBottom: 4,
    gap: 8,
  },
});
