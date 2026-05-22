import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  TouchableOpacity,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

interface ComposerSendButtonProps {
  icon: IoniconName;
  accessibilityLabel: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  enabled: boolean;
  loading: boolean;
  compact: boolean;
  onPress(): void;
}

export function ComposerSendButton({
  icon,
  accessibilityLabel,
  chrome,
  theme,
  enabled,
  loading,
  compact,
  onPress,
}: ComposerSendButtonProps) {
  return (
    <TouchableOpacity
      accessibilityLabel={accessibilityLabel}
      accessibilityRole="button"
      accessibilityState={{ disabled: !enabled, busy: loading }}
      style={[
        styles.button,
        {
          backgroundColor: enabled ? chrome.text : chrome.surfaceMuted,
          borderColor: enabled ? chrome.text : chrome.border,
        },
        !enabled ? styles.disabled : null,
      ]}
      onPress={onPress}
      disabled={!enabled}
      activeOpacity={0.8}
    >
      {loading ? (
        <ActivityIndicator size="small" color={theme.background} />
      ) : (
        <Ionicons
          name={icon}
          size={compact ? 12 : 18}
          color={enabled ? theme.background : chrome.textSubtle}
        />
      )}
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  button: {
    width: 38,
    height: 38,
    borderRadius: 19,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  disabled: {
    opacity: 0.62,
  },
});
