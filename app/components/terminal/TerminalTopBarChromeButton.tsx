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
  onPress(): void;
}

export function TerminalTopBarChromeButton({
  accessibilityLabel,
  chrome,
  icon,
  onPress,
}: TerminalTopBarChromeButtonProps) {
  return (
    <TouchableOpacity
      accessibilityLabel={accessibilityLabel}
      accessibilityRole="button"
      onPress={onPress}
      style={styles.button}
      activeOpacity={0.75}
    >
      <Ionicons name={icon} size={20} color={chrome.textMuted} />
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
});
