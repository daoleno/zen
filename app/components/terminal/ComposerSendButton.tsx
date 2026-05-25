import { Ionicons } from "@expo/vector-icons";
import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
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
  running: boolean;
  elapsedLabel?: string;
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
  elapsedLabel,
  onPress,
}: ComposerSendButtonProps) {
  const animated = loading || running;
  const foreground = running
    ? chrome.text
    : enabled && !loading
      ? theme.background
      : chrome.textSubtle;
  const backgroundColor = enabled && !loading && !running
    ? chrome.text
    : chrome.surfaceMuted;
  const borderColor = running
    ? chrome.border
    : enabled && !loading
      ? chrome.text
      : "transparent";

  return (
    <TouchableOpacity
      accessibilityLabel={
        elapsedLabel ? `${accessibilityLabel}, ${elapsedLabel}` : accessibilityLabel
      }
      accessibilityRole="button"
      accessibilityState={{ disabled: !enabled, busy: animated }}
      style={[
        styles.button,
        elapsedLabel ? styles.buttonWithLabel : null,
        { backgroundColor, borderColor },
        !enabled && !loading ? styles.disabled : null,
      ]}
      onPress={onPress}
      activeOpacity={0.78}
      disabled={!enabled}
    >
      {loading ? (
        <ActivityIndicator size="small" color={chrome.textSubtle} />
      ) : running ? (
        <View style={styles.runningContent}>
          <Ionicons name="square" size={8} color={foreground} />
          {elapsedLabel ? (
            <Text style={[styles.elapsedLabel, { color: chrome.textSubtle }]}>
              {elapsedLabel}
            </Text>
          ) : null}
        </View>
      ) : (
        <Ionicons
          name={icon}
          size={18}
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
  buttonWithLabel: {
    minWidth: 58,
    paddingHorizontal: 10,
    width: "auto",
  },
  runningContent: {
    alignItems: "center",
    flexDirection: "row",
  },
  elapsedLabel: {
    fontSize: 11,
    lineHeight: 14,
    marginLeft: 6,
  },
  disabled: {
    opacity: 0.62,
  },
});
