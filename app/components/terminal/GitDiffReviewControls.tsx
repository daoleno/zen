import React from "react";
import { ActivityIndicator, Alert, Pressable, StyleSheet } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

export function DiffIconButton({
  icon,
  label,
  onPress,
  chrome,
  disabled = false,
  selected = false,
  busy = false,
  accentColor,
}: {
  icon: React.ComponentProps<typeof Ionicons>["name"];
  label: string;
  onPress(): void;
  chrome: TerminalThemeChrome;
  disabled?: boolean;
  selected?: boolean;
  busy?: boolean;
  accentColor?: string;
}) {
  const accent = accentColor ?? chrome.accent;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityHint={label}
      accessibilityState={{ disabled, selected, busy }}
      disabled={disabled}
      onPress={onPress}
      onLongPress={() => Alert.alert(label)}
      style={({ pressed }) => [
        styles.button,
        {
          opacity: disabled ? 0.4 : pressed ? 0.6 : 1,
          backgroundColor: selected ? chrome.surfaceMuted : "transparent",
          borderColor: selected ? chrome.border : "transparent",
        },
      ]}
    >
      {busy ? (
        <ActivityIndicator size="small" color={accent} />
      ) : (
        <Ionicons
          name={icon}
          size={20}
          color={selected ? accent : chrome.text}
        />
      )}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  button: {
    width: 44,
    height: 44,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
  },
});
