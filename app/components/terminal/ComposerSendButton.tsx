import { Ionicons } from "@expo/vector-icons";
import React from "react";
import {
  StyleSheet,
  TouchableOpacity,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { ComposerLoadingDots } from "./ComposerLoadingDots";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

interface ComposerSendButtonProps {
  icon: IoniconName;
  accessibilityLabel: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  enabled: boolean;
  loading: boolean;
  running: boolean;
  onPress(): void;
}

export function ComposerSendButton({
  icon,
  accessibilityLabel,
  chrome,
  theme,
  enabled,
  loading,
  running,
  onPress,
}: ComposerSendButtonProps) {
  const animated = loading || running;
  const foreground = running || (enabled && !loading)
    ? theme.background
    : loading
      ? chrome.text
      : chrome.textSubtle;
  const backgroundColor = running || (enabled && !loading)
    ? chrome.text
    : loading
      ? chrome.surface
      : chrome.surfaceMuted;
  const borderColor = running || (enabled && !loading)
    ? chrome.text
    : loading
      ? chrome.borderStrong
      : chrome.border;

  return (
    <TouchableOpacity
      accessibilityLabel={accessibilityLabel}
      accessibilityRole="button"
      accessibilityState={{ disabled: !enabled, busy: animated }}
      style={[
        styles.button,
        { backgroundColor, borderColor },
        !enabled && !loading ? styles.disabled : null,
      ]}
      onPress={onPress}
      activeOpacity={0.78}
      disabled={!enabled}
    >
      {loading ? (
        <ComposerLoadingDots
          color={chrome.accent}
          size={4}
          gap={3}
        />
      ) : (
        <Ionicons
          name={icon}
          size={running ? 11 : 18}
          color={foreground}
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
