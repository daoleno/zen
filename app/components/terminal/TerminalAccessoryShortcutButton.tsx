import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
} from "react-native";
import { Typography } from "../../constants/tokens";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

interface TerminalAccessoryShortcutButtonProps {
  label: string;
  chrome: TerminalThemeChrome;
  active?: boolean;
  delayLongPress?: number;
  onPress?(): void;
  onPressIn?(): void;
  onPressOut?(): void;
}

export function TerminalAccessoryShortcutButton({
  label,
  chrome,
  active = false,
  delayLongPress,
  onPress,
  onPressIn,
  onPressOut,
}: TerminalAccessoryShortcutButtonProps) {
  return (
    <TouchableOpacity
      accessibilityLabel={label}
      accessibilityRole="button"
      style={styles.shortcutBtn}
      onPress={onPress}
      onPressIn={onPressIn}
      onPressOut={onPressOut}
      delayLongPress={delayLongPress}
      activeOpacity={0.6}
    >
      <Text
        style={[
          styles.shortcutText,
          { color: active ? chrome.accent : chrome.textMuted },
        ]}
      >
        {label}
      </Text>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  shortcutBtn: {
    paddingHorizontal: 10,
    minHeight: 44,
    marginRight: 2,
    alignItems: "center",
    justifyContent: "center",
  },
  shortcutText: {
    fontSize: 13,
    fontFamily: Typography.terminalFont,
  },
});
