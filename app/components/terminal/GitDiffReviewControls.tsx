import React from "react";
import { Alert, Pressable, StyleSheet } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

export function DiffIconButton({
  icon,
  label,
  onPress,
  chrome,
  disabled = false,
  selected = false,
}: {
  icon: React.ComponentProps<typeof Ionicons>["name"];
  label: string;
  onPress(): void;
  chrome: TerminalThemeChrome;
  disabled?: boolean;
  selected?: boolean;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled, selected }}
      disabled={disabled}
      onPress={onPress}
      onLongPress={() => Alert.alert(label)}
      style={({ pressed }) => [
        styles.button,
        {
          opacity: disabled ? 0.3 : pressed ? 0.6 : 1,
          backgroundColor: selected ? chrome.surfaceMuted : "transparent",
        },
      ]}
    >
      <Ionicons
        name={icon}
        size={20}
        color={selected ? chrome.accent : chrome.text}
      />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  button: {
    width: 44,
    height: 44,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: 4,
  },
});
