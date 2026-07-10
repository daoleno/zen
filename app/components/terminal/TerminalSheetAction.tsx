import React from "react";
import { StyleSheet, Text } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { AnimatedPressable } from "../ui/AnimatedPressable";

export type TerminalSheetActionIcon = React.ComponentProps<typeof Ionicons>["name"];

interface TerminalSheetActionProps {
  icon: TerminalSheetActionIcon;
  label: string;
  disabled?: boolean;
  destructive?: boolean;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onPress(): void;
}

export function TerminalSheetAction({
  icon,
  label,
  onPress,
  disabled = false,
  destructive = false,
  chrome,
}: TerminalSheetActionProps) {
  const color = disabled
    ? chrome.textSubtle
    : destructive
      ? chrome.danger
      : chrome.text;

  return (
    <AnimatedPressable
      accessibilityLabel={label}
      accessibilityRole="button"
      accessibilityState={{ disabled }}
      style={[
        styles.action,
        disabled ? { backgroundColor: chrome.disabledSurface } : null,
      ]}
      preset="press"
      scale={0.97}
      disabled={disabled}
      onPress={() => {
        if (!disabled) {
          Haptics.impactAsync(
            destructive
              ? Haptics.ImpactFeedbackStyle.Medium
              : Haptics.ImpactFeedbackStyle.Light,
          );
          onPress();
        }
      }}
    >
      <Ionicons name={icon} size={16} color={color} />
      <Text style={[styles.label, { color }]} numberOfLines={1}>
        {label}
      </Text>
    </AnimatedPressable>
  );
}

const styles = StyleSheet.create({
  action: {
    minHeight: 44,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingHorizontal: 12,
    paddingVertical: 8,
  },
  label: {
    flex: 1,
    fontSize: 14,
    fontFamily: Typography.uiFont,
  },
});
