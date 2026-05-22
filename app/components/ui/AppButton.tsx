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
import { AppText } from "./AppText";

type AppButtonVariant = "primary" | "secondary" | "ghost";
type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

interface AppButtonProps extends Omit<PressableProps, "style" | "children"> {
  label: string;
  variant?: AppButtonVariant;
  icon?: IoniconName;
  style?: StyleProp<ViewStyle>;
}

export function AppButton({
  label,
  variant = "secondary",
  icon,
  disabled,
  style,
  ...props
}: AppButtonProps) {
  const colors = useAppColors();
  const buttonStyle =
    variant === "primary"
      ? { backgroundColor: colors.accent, borderColor: colors.accent }
      : variant === "secondary"
        ? { backgroundColor: colors.surfaceSubtle, borderColor: colors.borderSubtle }
        : { backgroundColor: "transparent", borderColor: "transparent" };
  const textTone = variant === "primary" ? "onAccent" : "secondary";
  const iconColor = variant === "primary" ? colors.textOnAccent : colors.textSecondary;

  return (
    <Pressable
      {...props}
      disabled={disabled}
      style={({ pressed }) => [
        styles.button,
        buttonStyle,
        pressed && !disabled ? styles.pressed : null,
        disabled ? styles.disabled : null,
        style,
      ]}
    >
      {icon ? <Ionicons name={icon} size={16} color={iconColor} /> : null}
      <AppText variant="button" tone={textTone}>
        {label}
      </AppText>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  button: {
    minHeight: 40,
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 8,
  },
  pressed: {
    opacity: 0.72,
  },
  disabled: {
    opacity: 0.5,
  },
});
