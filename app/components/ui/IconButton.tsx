import React from "react";
import {
  Pressable,
  type PressableProps,
  type StyleProp,
  StyleSheet,
  type ViewStyle,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import { useAppColors } from "../../constants/tokens";
import { AnimatedPressable } from "./AnimatedPressable";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];
type IconButtonTone = "default" | "input" | "ghost";

interface IconButtonProps extends Omit<PressableProps, "style" | "children"> {
  icon: IoniconName;
  size?: number;
  iconSize?: number;
  color?: string;
  tone?: IconButtonTone;
  /** Light haptic on press. Defaults to true for ghost/default tones. */
  haptic?: boolean;
  style?: StyleProp<ViewStyle>;
}

export function IconButton({
  icon,
  size = 36,
  iconSize = 18,
  color,
  tone = "default",
  haptic = true,
  disabled,
  style,
  onPress,
  ...props
}: IconButtonProps) {
  const colors = useAppColors();
  const backgroundColor =
    tone === "input"
      ? colors.inputBackground
      : tone === "ghost"
        ? "transparent"
        : colors.bgElevated;
  const borderColor = tone === "ghost" ? "transparent" : colors.borderSubtle;

  return (
    <AnimatedPressable
      {...props}
      preset="press"
      scale={0.92}
      disabled={disabled}
      onPress={(e) => {
        if (haptic && !disabled) {
          Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
        }
        onPress?.(e);
      }}
      style={[
        styles.button,
        {
          width: size,
          minHeight: size,
          borderRadius: Math.max(10, Math.round(size / 3)),
          backgroundColor,
          borderColor,
        },
        style,
      ]}
    >
      <Ionicons name={icon} size={iconSize} color={color ?? colors.textSecondary} />
    </AnimatedPressable>
  );
}

const styles = StyleSheet.create({
  button: {
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
});
