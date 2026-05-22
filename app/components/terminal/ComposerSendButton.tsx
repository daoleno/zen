import React from "react";
import {
  StyleSheet,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { ComposerIconButton } from "./ComposerIconButton";

interface ComposerSendButtonProps {
  icon: React.ComponentProps<typeof ComposerIconButton>["icon"];
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
    <ComposerIconButton
      accessibilityLabel={accessibilityLabel}
      icon={icon}
      chrome={chrome}
      iconSize={compact ? 12 : 18}
      iconColor={enabled ? theme.background : chrome.textSubtle}
      loading={loading}
      loadingColor={theme.background}
      disabled={!enabled}
      disabledOpacity={0.62}
      style={[
        styles.button,
        {
          backgroundColor: enabled ? chrome.text : chrome.surfaceMuted,
          borderColor: enabled ? chrome.text : chrome.border,
        },
      ]}
      onPress={onPress}
    />
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
});
