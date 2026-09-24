import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  View,
  type PressableProps,
  type StyleProp,
  type ViewStyle,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import {
  ContinuousCorners,
  shadow,
  useAppTheme,
} from "../../constants/tokens";
import { AnimatedPressable } from "./AnimatedPressable";
import { AppText } from "./AppText";

export type ButtonVariant = "filled" | "tinted" | "plain" | "destructive";
export type ButtonSize = "sm" | "md" | "lg";
type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

export interface ButtonProps extends Omit<PressableProps, "style" | "children"> {
  label: string;
  variant?: ButtonVariant;
  size?: ButtonSize;
  icon?: IoniconName;
  loading?: boolean;
  /** Stretch to the container width. */
  block?: boolean;
  haptic?: boolean;
  style?: StyleProp<ViewStyle>;
}

const HEIGHT: Record<ButtonSize, number> = { sm: 36, md: 48, lg: 54 };
const ICON: Record<ButtonSize, number> = { sm: 15, md: 17, lg: 18 };

/** Capsule button shared by forms, sheets, empty states and dialogs. */
export function Button({
  label,
  variant = "tinted",
  size = "md",
  icon,
  loading = false,
  block = false,
  haptic = true,
  disabled,
  style,
  onPress,
  accessibilityState,
  ...props
}: ButtonProps) {
  const { colors, theme } = useAppTheme();
  const inactive = Boolean(disabled || loading);
  const palette = inactive && !loading
    ? { fill: colors.disabledSurface, text: colors.disabledText }
    : variant === "filled"
      ? { fill: colors.accent, text: colors.textOnAccent }
      : variant === "destructive"
        ? { fill: colors.dangerSoft, text: colors.dangerText }
        : variant === "tinted"
          ? { fill: theme.materials.tint, text: colors.accentStrong }
          : { fill: "transparent", text: colors.accentStrong };

  return (
    <AnimatedPressable
      {...props}
      accessibilityRole="button"
      accessibilityLabel={props.accessibilityLabel ?? label}
      accessibilityState={{ ...accessibilityState, disabled: inactive, busy: loading }}
      disabled={inactive}
      scale={0.97}
      onPress={(event) => {
        if (haptic) void Haptics.selectionAsync();
        onPress?.(event);
      }}
      style={[
        styles.button,
        {
          minHeight: HEIGHT[size],
          paddingHorizontal: size === "sm" ? 14 : 20,
          backgroundColor: palette.fill,
        },
        block && styles.block,
        variant === "filled" && !inactive ? shadow("raised", colors.accent) : null,
        style,
      ]}
    >
      <View style={styles.content}>
        {loading ? (
          <ActivityIndicator size="small" color={palette.text} />
        ) : icon ? (
          <Ionicons name={icon} size={ICON[size]} color={palette.text} />
        ) : null}
        <AppText
          variant={size === "sm" ? "label" : "button"}
          numberOfLines={1}
          style={[styles.label, { color: palette.text }]}
        >
          {label}
        </AppText>
      </View>
    </AnimatedPressable>
  );
}

const styles = StyleSheet.create({
  button: {
    borderRadius: 999,
    ...ContinuousCorners,
    alignItems: "center",
    justifyContent: "center",
    maxWidth: "100%",
  },
  block: {
    alignSelf: "stretch",
  },
  content: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 8,
  },
  label: {
    flexShrink: 1,
    textAlign: "center",
  },
});
