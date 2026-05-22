import React from "react";
import {
  StyleSheet,
  TouchableOpacity,
} from "react-native";

type AccessibilityState = React.ComponentProps<
  typeof TouchableOpacity
>["accessibilityState"];

interface TerminalAccessoryIconButtonProps {
  accessibilityLabel: string;
  accessibilityState?: AccessibilityState;
  disabled?: boolean;
  children: React.ReactNode;
  onPress(): void;
}

export function TerminalAccessoryIconButton({
  accessibilityLabel,
  accessibilityState,
  disabled = false,
  children,
  onPress,
}: TerminalAccessoryIconButtonProps) {
  return (
    <TouchableOpacity
      accessibilityLabel={accessibilityLabel}
      accessibilityRole="button"
      accessibilityState={accessibilityState}
      style={[styles.button, disabled ? styles.disabled : null]}
      onPress={onPress}
      disabled={disabled}
      activeOpacity={0.75}
    >
      {children}
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  button: {
    width: 36,
    height: 36,
    marginRight: 2,
    alignItems: "center",
    justifyContent: "center",
  },
  disabled: {
    opacity: 0.35,
  },
});
