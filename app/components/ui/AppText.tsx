import React from "react";
import { Text, type StyleProp, StyleSheet, type TextProps, type TextStyle } from "react-native";
import {
  Typography,
  UiTextMetrics,
  uiLineHeight,
  useAppColors,
} from "../../constants/tokens";

type AppTextVariant =
  | "display"
  | "sheetTitle"
  | "title"
  | "subtitle"
  | "label"
  | "body"
  | "caption"
  | "mono"
  | "button";

type AppTextTone = "primary" | "secondary" | "muted" | "danger" | "accent" | "onAccent";

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
    case "muted":
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
  display: {
    fontSize: 30,
    lineHeight: uiLineHeight(30),
    fontFamily: Typography.uiFontMedium,
  },
  sheetTitle: {
    fontSize: 20,
    lineHeight: uiLineHeight(20),
    fontFamily: Typography.uiFontMedium,
  },
  title: {
    fontSize: 16,
    lineHeight: uiLineHeight(16),
    fontFamily: Typography.uiFontMedium,
  },
  subtitle: {
    fontSize: 14,
    lineHeight: uiLineHeight(14),
    fontFamily: Typography.uiFont,
  },
  label: {
    fontSize: 12,
    lineHeight: uiLineHeight(12),
    fontFamily: Typography.uiFontMedium,
  },
  body: {
    fontSize: 14,
    lineHeight: uiLineHeight(14),
    fontFamily: Typography.uiFont,
  },
  caption: {
    fontSize: 12,
    lineHeight: uiLineHeight(12),
    fontFamily: Typography.uiFont,
  },
  mono: {
    fontSize: 13,
    lineHeight: uiLineHeight(13),
    fontFamily: Typography.terminalFont,
  },
  button: {
    fontSize: 14,
    lineHeight: uiLineHeight(14),
    fontFamily: Typography.uiFontMedium,
  },
});
