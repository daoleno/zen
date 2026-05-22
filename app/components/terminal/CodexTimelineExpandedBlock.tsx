import React from "react";
import {
  StyleSheet,
  View,
} from "react-native";
import type {
  StyleProp,
  ViewStyle,
} from "react-native";

interface CodexTimelineExpandedBlockProps {
  borderColor: string;
  children: React.ReactNode;
  style?: StyleProp<ViewStyle>;
}

export function CodexTimelineExpandedBlock({
  borderColor,
  children,
  style,
}: CodexTimelineExpandedBlockProps) {
  return (
    <View style={[styles.expanded, style, { borderColor }]}>
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  expanded: {
    marginTop: 6,
    marginLeft: 19,
    maxWidth: "92%",
    borderLeftWidth: StyleSheet.hairlineWidth,
    paddingLeft: 10,
    paddingVertical: 4,
  },
});
