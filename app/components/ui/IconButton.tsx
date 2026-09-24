import React from "react";
import {
  type PressableProps,
  type StyleProp,
  StyleSheet,
  type ViewStyle,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import { ContinuousCorners, useAppTheme } from "../../constants/tokens";
import { AnimatedPressable } from "./AnimatedPressable";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];
type IconButtonTone = "default" | "input" | "ghost" | "tinted";

interface IconButtonProps extends Omit<PressableProps, "style" | "children"> {
  icon: IoniconName;
  size?: number;
  iconSize?: number;
  color?: string;
  tone?: IconButtonTone;
  /** Light haptic on press. */
  haptic?: boolean;
  style?: StyleProp<ViewStyle>;
}

/** Circular icon control; `default` is a thin material capsule. */
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
  hitSlop,
  ...props
}: IconButtonProps) {
  const { colors, theme } = useAppTheme();
  const backgroundColor =
    tone === "input"
      ? colors.inputBackground
      : tone === "ghost"
        ? "transparent"
        : tone === "tinted"
          ? theme.materials.tint
          : theme.materials.thin;
  const borderColor =
    tone === "ghost" || tone === "tinted" ? "transparent" : theme.materials.stroke;
  const iconColor =
    color ?? (tone === "tinted" ? colors.accentStrong : colors.textSecondary);
  const slop = hitSlop ?? Math.max(0, Math.ceil((44 - size) / 2));

  return (
    <AnimatedPressable
      {...props}
      accessibilityRole={props.accessibilityRole ?? "button"}
      hitSlop={slop}
      preset="press"
      scale={0.9}
      disabled={disabled}
      onPress={(e) => {
        if (haptic && !disabled) {
          void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
        }
        onPress?.(e);
      }}
      style={[
        styles.button,
        {
          width: size,
          height: size,
          borderRadius: size / 2,
          backgroundColor,
          borderColor,
        },
        style,
      ]}
    >
      <Ionicons name={icon} size={iconSize} color={iconColor} />
    </AnimatedPressable>
  );
}

const styles = StyleSheet.create({
  button: {
    ...ContinuousCorners,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
});
