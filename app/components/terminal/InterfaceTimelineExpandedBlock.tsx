import React from "react";
import { StyleSheet, View } from "react-native";
import type { StyleProp, ViewStyle } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

interface InterfaceTimelineExpandedBlockProps {
  chrome: TerminalThemeChrome;
  children: React.ReactNode;
  style?: StyleProp<ViewStyle>;
  borderColor?: string;
}
export function InterfaceTimelineExpandedBlock({
  chrome,
  borderColor,
  children,
  style,
}: InterfaceTimelineExpandedBlockProps) {
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

/**
 * Details hang from the header's 18 pt tone mark: the leading rail runs
 * through the mark's center (9 pt) and the content aligns with the header
 * title (18 pt slot + 8 pt gap), so expanded tool output reads as one
 * subordinate thread beneath its row.
 */
const RAIL_WIDTH = 2;
const RAIL_CENTER = 9;
const TITLE_INSET = 26;

const styles = StyleSheet.create({
  expanded: {
    marginTop: 2,
    marginBottom: 4,
    marginLeft: RAIL_CENTER - RAIL_WIDTH / 2,
    maxWidth: "96%",
    borderLeftWidth: RAIL_WIDTH,
    paddingLeft: TITLE_INSET - RAIL_CENTER - RAIL_WIDTH / 2,
    paddingRight: 4,
    paddingTop: 6,
    paddingBottom: 6,
    gap: 10,
  },
});
