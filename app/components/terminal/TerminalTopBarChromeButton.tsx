import React from "react";
import {
  StyleSheet,
  TouchableOpacity,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

interface TerminalTopBarChromeButtonProps {
  accessibilityLabel: string;
  chrome: TerminalThemeChrome;
  icon: IoniconName;
  disabled?: boolean;
  onPress(): void;
}

export function TerminalTopBarChromeButton({
  accessibilityLabel,
  chrome,
  icon,
  disabled = false,
  onPress,
}: TerminalTopBarChromeButtonProps) {
  return (
    <TouchableOpacity
      accessibilityLabel={accessibilityLabel}
      accessibilityRole="button"
      accessibilityState={{ disabled }}
      onPress={onPress}
      disabled={disabled}
      style={[styles.button, disabled ? styles.disabled : null]}
      activeOpacity={0.75}
    >
      <Ionicons name={icon} size={20} color={disabled ? chrome.textSubtle : chrome.textMuted} />
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  button: {
    width: 32,
    height: 32,
    alignItems: "center",
    justifyContent: "center",
  },
  disabled: {
    opacity: 0.48,
  },
});
