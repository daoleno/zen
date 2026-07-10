import React from "react";
import {
  StyleSheet,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { AnimatedPressable } from "../ui/AnimatedPressable";

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
    <AnimatedPressable
      accessibilityLabel={accessibilityLabel}
      accessibilityRole="button"
      accessibilityState={{ disabled }}
      onPress={() => {
        if (!disabled) {
          Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
          onPress();
        }
      }}
      disabled={disabled}
      style={styles.button}
      preset="press"
      scale={0.88}
    >
      <Ionicons name={icon} size={20} color={disabled ? chrome.textSubtle : chrome.textMuted} />
    </AnimatedPressable>
  );
}

const styles = StyleSheet.create({
  button: {
    width: 44,
    height: 44,
    alignItems: "center",
    justifyContent: "center",
    opacity: 1,
  },
});
