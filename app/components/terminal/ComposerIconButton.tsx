import React from "react";
import { StyleSheet, TouchableOpacity, View } from "react-native";
import type { StyleProp, ViewStyle } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { ContinuousCorners } from "../../constants/tokens";
import { ComposerLoadingDots } from "./ComposerLoadingDots";
import { COMPOSER_LEADING_ACTION_WIDTH } from "./composerActionSlot";
import {
  COMPOSER_CONTROL_DISC_SIZE,
  composerNeutralFill,
} from "./composerMaterial";

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
  /**
   * `tinted` draws a quiet 34 pt disc inside the 44 pt target (the leading
   * Plus in the expanding dock); `plain` is glyph-only.
   */
  variant?: "plain" | "tinted";
  /** Tinted only: the control's panel is open (e.g. the action menu). */
  active?: boolean;
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
  variant = "plain",
  active = false,
  style,
  onPress,
}: ComposerIconButtonProps) {
  const glyph = loading ? (
    <ComposerLoadingDots color={loadingColor ?? chrome.accent} size={8} />
  ) : (
    <Ionicons name={icon} size={iconSize} color={iconColor ?? chrome.text} />
  );
  return (
    <TouchableOpacity
      accessibilityLabel={accessibilityLabel}
      accessibilityRole="button"
      accessibilityState={{
        disabled,
        busy: loading,
        expanded: variant === "tinted" ? active : undefined,
      }}
      style={[
        styles.button,
        style,
      ]}
      onPress={onPress}
      activeOpacity={0.72}
      disabled={disabled}
    >
      {variant === "tinted" ? (
        <View
          style={[
            styles.disc,
            {
              backgroundColor: active
                ? chrome.accentSoft
                : composerNeutralFill(chrome),
              opacity: disabled && !loading ? 0.55 : 1,
            },
          ]}
        >
          {glyph}
        </View>
      ) : (
        glyph
      )}
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  button: {
    width: COMPOSER_LEADING_ACTION_WIDTH,
    height: 44,
    borderRadius: 22,
    alignItems: "center",
    justifyContent: "center",
  },
  disc: {
    width: COMPOSER_CONTROL_DISC_SIZE,
    height: COMPOSER_CONTROL_DISC_SIZE,
    borderRadius: COMPOSER_CONTROL_DISC_SIZE / 2,
    ...ContinuousCorners,
    alignItems: "center",
    justifyContent: "center",
  },
});
