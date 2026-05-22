import React from "react";
import { StyleSheet, Text, TouchableOpacity } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";

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
  theme,
}: TerminalSheetActionProps) {
  const color = disabled
    ? chrome.textSubtle
    : destructive
      ? theme.red
      : chrome.text;

  return (
    <TouchableOpacity
      accessibilityLabel={label}
      accessibilityRole="button"
      accessibilityState={{ disabled }}
      style={[styles.action, disabled ? styles.disabled : null]}
      onPress={onPress}
      disabled={disabled}
      activeOpacity={0.84}
    >
      <Ionicons name={icon} size={16} color={color} />
      <Text style={[styles.label, { color }]} numberOfLines={1}>
        {label}
      </Text>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  action: {
    minHeight: 38,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingHorizontal: 12,
    paddingVertical: 8,
  },
  disabled: {
    opacity: 0.52,
  },
  label: {
    flex: 1,
    fontSize: 14,
    fontFamily: Typography.uiFont,
  },
});
