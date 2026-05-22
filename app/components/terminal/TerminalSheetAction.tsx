import React from "react";
import { StyleSheet, Text, TouchableOpacity } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Colors, Typography } from "../../constants/tokens";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

interface TerminalSheetActionProps {
  icon: IoniconName;
  label: string;
  disabled?: boolean;
  destructive?: boolean;
  textColor: string;
  disabledTextColor: string;
  destructiveColor: string;
  onPress(): void;
}

export function TerminalSheetAction({
  icon,
  label,
  onPress,
  disabled = false,
  destructive = false,
  textColor,
  disabledTextColor,
  destructiveColor,
}: TerminalSheetActionProps) {
  const color = disabled
    ? disabledTextColor
    : destructive
      ? destructiveColor
      : textColor;

  return (
    <TouchableOpacity
      style={[styles.action, disabled ? styles.disabled : null]}
      onPress={onPress}
      disabled={disabled}
      activeOpacity={0.84}
    >
      <Ionicons name={icon} size={16} color={color} />
      <Text style={[styles.label, { color }]}>{label}</Text>
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
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFont,
  },
});
