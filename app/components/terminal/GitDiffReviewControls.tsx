import React from "react";
import { ActivityIndicator, Alert, Pressable, StyleSheet, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { ContinuousCorners, TouchTarget } from "../../constants/tokens";
import { withAlpha } from "./colorWithAlpha";

const GLYPH_DISC = 36;

/**
 * Icon control for Git review chrome. The press target is always the platform
 * minimum (44pt iOS, 48dp Android); `filled` draws a quiet material disc
 * behind the glyph for the sheet's leading and overflow buttons.
 */
export function DiffIconButton({
  icon,
  label,
  onPress,
  chrome,
  disabled = false,
  selected = false,
  busy = false,
  filled = false,
  accentColor,
}: {
  icon: React.ComponentProps<typeof Ionicons>["name"];
  label: string;
  onPress(): void;
  chrome: TerminalThemeChrome;
  disabled?: boolean;
  selected?: boolean;
  busy?: boolean;
  filled?: boolean;
  accentColor?: string;
}) {
  const accent = accentColor ?? chrome.accent;
  const discColor = selected
    ? withAlpha(accent, 0.16)
    : filled
      ? withAlpha(chrome.text, 0.07)
      : "transparent";
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityHint={label}
      accessibilityState={{ disabled, selected, busy }}
      disabled={disabled}
      onPress={() => {
        void Haptics.selectionAsync();
        onPress();
      }}
      onLongPress={() => Alert.alert(label)}
      style={({ pressed }) => [
        styles.button,
        { opacity: disabled ? 0.38 : pressed ? 0.55 : 1 },
      ]}
    >
      <View style={[styles.disc, { backgroundColor: discColor }]}>
        {busy ? (
          <ActivityIndicator size="small" color={chrome.textMuted} />
        ) : (
          <Ionicons
            name={icon}
            size={20}
            color={selected ? accent : chrome.text}
          />
        )}
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  button: {
    width: TouchTarget,
    height: TouchTarget,
    alignItems: "center",
    justifyContent: "center",
  },
  disc: {
    width: GLYPH_DISC,
    height: GLYPH_DISC,
    borderRadius: GLYPH_DISC / 2,
    ...ContinuousCorners,
    alignItems: "center",
    justifyContent: "center",
  },
});
