import React from "react";
import {
  Pressable,
  type PressableProps,
  type StyleProp,
  StyleSheet,
  type ViewStyle,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useAppColors } from "../../constants/tokens";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];
type IconButtonTone = "default" | "input" | "ghost";

interface IconButtonProps extends Omit<PressableProps, "style" | "children"> {
  icon: IoniconName;
  size?: number;
  iconSize?: number;
  color?: string;
  tone?: IconButtonTone;
  style?: StyleProp<ViewStyle>;
}

export function IconButton({
  icon,
  size = 36,
  iconSize = 18,
  color,
  tone = "default",
  disabled,
  style,
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
    <Pressable
      {...props}
      disabled={disabled}
      hitSlop={8}
      style={({ pressed }) => [
        styles.button,
        {
          width: size,
          minHeight: size,
          borderRadius: Math.max(8, Math.round(size / 3)),
          backgroundColor,
          borderColor,
        },
        pressed && !disabled ? styles.pressed : null,
        disabled ? styles.disabled : null,
        style,
      ]}
    >
      <Ionicons name={icon} size={iconSize} color={color ?? colors.textSecondary} />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  button: {
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  pressed: {
    opacity: 0.72,
  },
  disabled: {
    opacity: 0.5,
  },
});
