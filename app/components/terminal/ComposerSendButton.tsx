import { Ionicons } from "@expo/vector-icons";
import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
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
  elapsedLabel?: string;
  variant?: "default" | "chatgpt";
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
  variant = "default",
  onPress,
}: ComposerSendButtonProps) {
  const chatgpt = variant === "chatgpt";
  const animated = loading || running;
  const foreground = running
    ? chrome.text
    : enabled && !loading
      ? "#FFFFFF"
      : chrome.textSubtle;
  const backgroundColor = enabled && !loading && !running
    ? chrome.accent
    : chrome.composerInput;
  const borderColor = running
    ? chrome.border
    : enabled && !loading
      ? chrome.accent
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
        chatgpt ? styles.buttonChatGpt : null,
        elapsedLabel ? styles.buttonWithLabel : null,
        { backgroundColor, borderColor },
        !enabled && !loading ? styles.disabled : null,
      ]}
      onPress={onPress}
      activeOpacity={0.78}
      disabled={!enabled}
    >
      {loading ? (
        <ComposerLoadingDots color={chrome.textSubtle} size={7} />
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
  buttonChatGpt: {
    width: 36,
    height: 36,
    borderRadius: 18,
  },
  button: {
    width: 40,
    height: 40,
    borderRadius: 20,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  buttonWithLabel: {
    minWidth: 66,
    paddingHorizontal: 11,
    width: "auto",
  },
  runningContent: {
    alignItems: "center",
    flexDirection: "row",
  },
  elapsedLabel: {
    fontFamily: Typography.chatMonoFont,
    fontSize: 11,
    lineHeight: 15,
    marginLeft: 6,
  },
  disabled: {
    opacity: 0.62,
  },
});
