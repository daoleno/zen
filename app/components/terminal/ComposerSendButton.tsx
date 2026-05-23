import { Ionicons } from "@expo/vector-icons";
import React, {
  useEffect,
  useRef,
} from "react";
import {
  Animated,
  Easing,
  StyleSheet,
  TouchableOpacity,
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
  const progress = useRef(new Animated.Value(0)).current;

  useEffect(() => {
    if (!animated) {
      progress.stopAnimation();
      progress.setValue(0);
      return;
    }

    const animation = Animated.loop(
      Animated.timing(progress, {
        toValue: 1,
        duration: running ? 1080 : 820,
        easing: Easing.linear,
        useNativeDriver: true,
      }),
    );
    animation.start();
    return () => {
      animation.stop();
      progress.stopAnimation();
      progress.setValue(0);
    };
  }, [animated, progress, running]);

  const rotation = progress.interpolate({
    inputRange: [0, 1],
    outputRange: ["0deg", "360deg"],
  });
  const pulseOpacity = progress.interpolate({
    inputRange: [0, 0.5, 1],
    outputRange: [0.62, 1, 0.62],
  });
  const foreground = running || (enabled && !loading)
    ? theme.background
    : loading
      ? chrome.accent
      : chrome.textSubtle;
  const backgroundColor = running || (enabled && !loading)
    ? chrome.text
    : loading
      ? chrome.accentSoft
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
      {animated ? (
        <>
          <Animated.View
            style={[
              styles.progressRing,
              {
                borderColor: running ? chrome.textSubtle : chrome.border,
                borderTopColor: foreground,
                borderRightColor: foreground,
                transform: [{ rotate: rotation }],
              },
            ]}
          />
          <Animated.View style={{ opacity: pulseOpacity }}>
            <Ionicons
              name={running ? "square" : "arrow-up"}
              size={running ? 10 : 14}
              color={foreground}
            />
          </Animated.View>
        </>
      ) : (
        <Ionicons name={icon} size={18} color={foreground} />
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
  progressRing: {
    position: "absolute",
    width: 29,
    height: 29,
    borderRadius: 15,
    borderWidth: 1.5,
  },
});
