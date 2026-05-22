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

export function ChatHeaderIconButton({
  icon,
  accessibilityLabel,
  chrome,
  color,
  disabled = false,
  onPress,
}: {
  icon: IoniconName;
  accessibilityLabel: string;
  chrome: TerminalThemeChrome;
  color?: string;
  disabled?: boolean;
  onPress(): void;
}) {
  return (
    <TouchableOpacity
      accessibilityLabel={accessibilityLabel}
      style={[
        styles.headerIconButton,
        {
          backgroundColor: "transparent",
          borderColor: "transparent",
        },
        disabled ? styles.disabled : null,
      ]}
      onPress={onPress}
      disabled={disabled}
      activeOpacity={0.75}
    >
      <Ionicons name={icon} size={16} color={color ?? chrome.textMuted} />
    </TouchableOpacity>
  );
}

export function ComposerIconButton({
  icon,
  accessibilityLabel,
  chrome,
  loading = false,
  disabled = false,
  iconColor,
  onPress,
}: {
  icon: IoniconName;
  accessibilityLabel: string;
  chrome: TerminalThemeChrome;
  loading?: boolean;
  disabled?: boolean;
  iconColor?: string;
  onPress(): void;
}) {
  return (
    <TouchableOpacity
      accessibilityLabel={accessibilityLabel}
      style={[
        styles.composerIconButton,
        disabled ? styles.composerIconButtonDisabled : null,
      ]}
      onPress={onPress}
      activeOpacity={0.78}
      disabled={disabled}
    >
      {loading ? (
        <ActivityIndicator size="small" color={chrome.accent} />
      ) : (
        <Ionicons name={icon} size={20} color={iconColor ?? chrome.text} />
      )}
    </TouchableOpacity>
  );
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
}: {
  icon: IoniconName;
  accessibilityLabel: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  enabled: boolean;
  loading: boolean;
  compact: boolean;
  onPress(): void;
}) {
  return (
    <TouchableOpacity
      accessibilityLabel={accessibilityLabel}
      style={[
        styles.sendButton,
        {
          backgroundColor: enabled ? chrome.text : chrome.surfaceMuted,
          borderColor: enabled ? chrome.text : chrome.border,
        },
        !enabled ? styles.sendButtonDisabled : null,
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
  headerIconButton: {
    width: 30,
    height: 30,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  disabled: {
    opacity: 0.54,
  },
  composerIconButton: {
    width: 36,
    height: 40,
    borderRadius: 20,
    alignItems: "center",
    justifyContent: "center",
  },
  composerIconButtonDisabled: {
    opacity: 0.54,
  },
  sendButton: {
    width: 38,
    height: 38,
    borderRadius: 19,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  sendButtonDisabled: {
    opacity: 0.62,
  },
});
