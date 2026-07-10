import React from "react";
import { Text, type StyleProp, StyleSheet, type TextProps, type TextStyle } from "react-native";
import {
  TypeScale,
  UiTextMetrics,
  useAppColors,
} from "../../constants/tokens";

type AppTextVariant =
  | "display"
  | "sheetTitle"
  | "title"
  | "heading"
  | "subtitle"
  | "compact"
  | "label"
  | "body"
  | "caption"
  | "micro"
  | "mono"
  | "monoStrong"
  | "button";

type AppTextTone =
  | "primary"
  | "secondary"
  | "tertiary"
  | "muted"
  | "disabled"
  | "danger"
  | "warning"
  | "success"
  | "accent"
  | "onAccent";

interface AppTextProps extends TextProps {
  variant?: AppTextVariant;
  tone?: AppTextTone;
  style?: StyleProp<TextStyle>;
}

export function AppText({
  variant = "body",
  tone = "primary",
  style,
  ...props
}: AppTextProps) {
  const colors = useAppColors();
  return (
    <Text
      {...props}
      style={[
        styles.base,
        variantStyles[variant],
        { color: toneColor(tone, colors) },
        style,
      ]}
    />
  );
}

function toneColor(tone: AppTextTone, colors: ReturnType<typeof useAppColors>) {
  switch (tone) {
    case "accent":
      return colors.accent;
    case "danger":
      return colors.dangerText;
    case "warning":
      return colors.warning;
    case "success":
      return colors.success;
    case "muted":
    case "tertiary":
      return colors.textTertiary;
    case "disabled":
      return colors.disabledText;
    case "secondary":
      return colors.textSecondary;
    case "onAccent":
      return colors.textOnAccent;
    case "primary":
      return colors.textPrimary;
  }
}

const styles = StyleSheet.create({
  base: UiTextMetrics,
});

const variantStyles = StyleSheet.create({
  display: TypeScale.display,
  sheetTitle: TypeScale.title,
  title: TypeScale.heading,
  heading: TypeScale.heading,
  subtitle: TypeScale.compact,
  compact: TypeScale.compact,
  label: TypeScale.label,
  body: TypeScale.body,
  caption: TypeScale.caption,
  micro: TypeScale.micro,
  mono: TypeScale.mono,
  monoStrong: TypeScale.monoStrong,
  button: TypeScale.label,
});
