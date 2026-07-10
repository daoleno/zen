import React from "react";
import { StyleSheet, Text } from "react-native";
import { Typography } from "../../constants/tokens";

interface PendingMessageLifecycleLabelProps {
  label: string;
  accessibilityLabel?: string;
  color: string;
}

export function PendingMessageLifecycleLabel({
  label,
  accessibilityLabel = label,
  color,
}: PendingMessageLifecycleLabelProps) {
  return (
    <Text
      accessibilityLabel={accessibilityLabel}
      accessibilityLiveRegion="polite"
      accessibilityRole="text"
      style={[styles.label, { color }]}
    >
      {label}
    </Text>
  );
}

const styles = StyleSheet.create({
  label: {
    fontFamily: Typography.uiFont,
    fontSize: 11,
    lineHeight: 14,
    includeFontPadding: false,
  },
});
