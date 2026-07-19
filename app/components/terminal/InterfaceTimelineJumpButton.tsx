import React from "react";
import { StyleSheet, TouchableOpacity, View, Text } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

interface InterfaceTimelineJumpButtonProps {
  bottom: number;
  chrome: TerminalThemeChrome;
  label?: string;
  onPress(): void;
}

export function InterfaceTimelineJumpButton({
  bottom,
  chrome,
  label,
  onPress,
}: InterfaceTimelineJumpButtonProps) {
  return (
    <TouchableOpacity
      accessibilityLabel={
        label ? `Jump to latest, ${label} ago` : "Jump to latest"
      }
      accessibilityRole="button"
      style={[
        styles.jumpButton,
        {
          backgroundColor: chrome.surfaceMuted,
          borderColor: chrome.borderStrong,
          bottom,
        },
      ]}
      onPress={onPress}
      activeOpacity={0.82}
    >
      <View style={styles.content}>
        <Ionicons name="arrow-down" size={18} color={chrome.accent} />
        {label ? (
          <Text style={[styles.label, { color: chrome.textSubtle }]}>
            {label}
          </Text>
        ) : null}
      </View>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  jumpButton: {
    position: "absolute",
    alignItems: "center",
    borderRadius: 22,
    borderWidth: StyleSheet.hairlineWidth,
    minHeight: 44,
    paddingHorizontal: 12,
    justifyContent: "center",
    right: 16,
    zIndex: 4,
  },
  content: {
    alignItems: "center",
    flexDirection: "row",
  },
  label: {
    fontSize: 12,
    lineHeight: 16,
    marginLeft: 6,
  },
});
