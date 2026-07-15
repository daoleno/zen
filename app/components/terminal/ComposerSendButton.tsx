import { Ionicons } from "@expo/vector-icons";
import React from "react";
import {
  StyleSheet,
  Text,
  View,
} from "react-native";
import { TouchableOpacity } from "react-native-gesture-handler";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { useElapsedDurationLabels } from "./useElapsedDurationLabel";
import { ComposerLoadingDots } from "./ComposerLoadingDots";
import { COMPOSER_ACTION_HORIZONTAL_PADDING } from "./composerActionSlot";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

interface ComposerSendButtonProps {
  icon: IoniconName;
  accessibilityLabel: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  enabled: boolean;
  loading: boolean;
  running: boolean;
  elapsedStartedAt?: string;
  variant?: "default" | "chatgpt";
  fixedWidth?: number;
  onPress(): void;
}

export function ComposerSendButton({
  icon,
  accessibilityLabel,
  chrome,
  enabled,
  loading,
  running,
  elapsedStartedAt,
  variant = "default",
  fixedWidth,
  onPress,
}: ComposerSendButtonProps) {
  const elapsedLabels = useElapsedDurationLabels(
    elapsedStartedAt,
    Boolean(elapsedStartedAt),
  );
  const elapsedLabel = running ? elapsedLabels.visual : "";
  const standalone = variant === "chatgpt";
  const animated = loading || running;
  const foreground = running
    ? chrome.text
    : enabled && !loading
      ? standalone
        ? chrome.textOnAccent
        : chrome.accent
      : chrome.textSubtle;
  const backgroundColor = standalone && enabled && !loading && !running
    ? chrome.accent
    : standalone
      ? chrome.composerInput
      : "transparent";
  const borderColor = standalone
    ? running
      ? chrome.border
      : enabled && !loading
        ? chrome.accent
        : "transparent"
    : "transparent";

  return (
    <TouchableOpacity
      accessibilityLabel={
        running && elapsedLabels.accessibility
          ? `${accessibilityLabel}, ${elapsedLabels.accessibility}`
          : accessibilityLabel
      }
      accessibilityRole="button"
      accessibilityState={{ disabled: !enabled, busy: animated }}
      style={[
        styles.button,
        standalone ? styles.buttonStandalone : null,
        elapsedLabel ? styles.buttonWithLabel : null,
        fixedWidth ? { width: fixedWidth, minWidth: fixedWidth, maxWidth: fixedWidth } : null,
        standalone ? { backgroundColor, borderColor } : null,
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
            <Text
              numberOfLines={1}
              style={[styles.elapsedLabel, { color: chrome.textMuted }]}
            >
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
    width: 44,
    height: 44,
    borderRadius: 22,
    alignItems: "center",
    justifyContent: "center",
  },
  buttonStandalone: {
    borderWidth: StyleSheet.hairlineWidth,
  },
  buttonWithLabel: {
    minWidth: 66,
    paddingHorizontal: COMPOSER_ACTION_HORIZONTAL_PADDING,
    width: "auto",
  },
  runningContent: {
    alignItems: "center",
    flexDirection: "row",
    maxWidth: "100%",
  },
  elapsedLabel: {
    fontFamily: Typography.chatMonoFont,
    fontSize: 11,
    lineHeight: 15,
    marginLeft: 6,
    flexShrink: 1,
  },
});
