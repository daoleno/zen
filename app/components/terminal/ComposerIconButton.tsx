import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  TouchableOpacity,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

interface ComposerIconButtonProps {
  icon: IoniconName;
  accessibilityLabel: string;
  chrome: TerminalThemeChrome;
  loading?: boolean;
  disabled?: boolean;
  iconColor?: string;
  onPress(): void;
}

export function ComposerIconButton({
  icon,
  accessibilityLabel,
  chrome,
  loading = false,
  disabled = false,
  iconColor,
  onPress,
}: ComposerIconButtonProps) {
  return (
    <TouchableOpacity
      accessibilityLabel={accessibilityLabel}
      accessibilityRole="button"
      accessibilityState={{ disabled, busy: loading }}
      style={[
        styles.button,
        disabled ? styles.disabled : null,
      ]}
      onPress={onPress}
      activeOpacity={0.78}
      disabled={disabled}
    >
      {loading ? (
        <ActivityIndicator size="small" color={chrome.accent} />
      ) : (
        <Ionicons name={icon} size={20} color={iconColor ?? chrome.text} />
      )}
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  button: {
    width: 36,
    height: 40,
    borderRadius: 20,
    alignItems: "center",
    justifyContent: "center",
  },
  disabled: {
    opacity: 0.54,
  },
});
