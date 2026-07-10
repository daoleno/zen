import React from "react";
import { StyleSheet } from "react-native";
import type { StyleProp, ViewStyle } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { TouchableOpacity } from "react-native-gesture-handler";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { ComposerLoadingDots } from "./ComposerLoadingDots";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

interface ComposerIconButtonProps {
  icon: IoniconName;
  accessibilityLabel: string;
  chrome: TerminalThemeChrome;
  loading?: boolean;
  disabled?: boolean;
  iconColor?: string;
  iconSize?: number;
  loadingColor?: string;
  style?: StyleProp<ViewStyle>;
  onPress(): void;
}

export function ComposerIconButton({
  icon,
  accessibilityLabel,
  chrome,
  loading = false,
  disabled = false,
  iconColor,
  iconSize = 20,
  loadingColor,
  style,
  onPress,
}: ComposerIconButtonProps) {
  return (
    <TouchableOpacity
      accessibilityLabel={accessibilityLabel}
      accessibilityRole="button"
      accessibilityState={{ disabled, busy: loading }}
      style={[
        styles.button,
        style,
      ]}
      onPress={onPress}
      activeOpacity={0.78}
      disabled={disabled}
    >
      {loading ? (
        <ComposerLoadingDots color={loadingColor ?? chrome.accent} size={8} />
      ) : (
        <Ionicons name={icon} size={iconSize} color={iconColor ?? chrome.text} />
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
});
