import React from "react";
import {
  ActivityIndicator,
  Pressable,
  StyleSheet,
  View,
  type StyleProp,
  type ViewStyle,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import { ContinuousCorners, Radii, useAppTheme } from "../../constants/tokens";
import { AppText } from "./AppText";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];
export type InlineNoticeTone = "neutral" | "accent" | "warning" | "danger";

interface InlineNoticeProps {
  title: string;
  detail?: string | null;
  tone?: InlineNoticeTone;
  icon?: IoniconName;
  busy?: boolean;
  action?: { label: string; onPress(): void; disabled?: boolean };
  style?: StyleProp<ViewStyle>;
}

/**
 * Compact in-flow status strip for connection loss, read-only threads and
 * recoverable errors. One line of title, optional detail, one trailing action.
 */
export function InlineNotice({
  title,
  detail,
  tone = "neutral",
  icon,
  busy = false,
  action,
  style,
}: InlineNoticeProps) {
  const { colors, theme } = useAppTheme();
  const palette = {
    neutral: { fill: colors.surfaceSubtle, ink: colors.textSecondary, glyph: "information-circle" as const },
    accent: { fill: theme.materials.tint, ink: colors.accentStrong, glyph: "information-circle" as const },
    warning: { fill: colors.warningSoft, ink: colors.warning, glyph: "cloud-offline" as const },
    danger: { fill: colors.dangerSoft, ink: colors.dangerText, glyph: "alert-circle" as const },
  }[tone];

  return (
    <View
      accessibilityRole="alert"
      accessibilityLiveRegion="polite"
      style={[styles.notice, { backgroundColor: palette.fill }, style]}
    >
      {busy ? (
        <ActivityIndicator size="small" color={palette.ink} style={styles.glyph} />
      ) : (
        <Ionicons name={icon ?? palette.glyph} size={17} color={palette.ink} style={styles.glyph} />
      )}
      <View style={styles.copy}>
        <AppText variant="label" numberOfLines={1} style={{ color: palette.ink }}>
          {title}
        </AppText>
        {detail ? (
          <AppText variant="caption" tone="secondary" numberOfLines={3}>
            {detail}
          </AppText>
        ) : null}
      </View>
      {action ? (
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={action.label}
          accessibilityState={{ disabled: Boolean(action.disabled) }}
          disabled={action.disabled}
          hitSlop={8}
          onPress={() => {
            void Haptics.selectionAsync();
            action.onPress();
          }}
          style={({ pressed }) => [
            styles.action,
            { backgroundColor: colors.bgElevated, opacity: action.disabled ? 0.5 : pressed ? 0.7 : 1 },
          ]}
        >
          <AppText variant="label" style={{ color: palette.ink }}>
            {action.label}
          </AppText>
        </Pressable>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  notice: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    minHeight: 44,
    paddingLeft: 12,
    paddingRight: 8,
    paddingVertical: 8,
    borderRadius: Radii.md,
    ...ContinuousCorners,
  },
  glyph: {
    width: 18,
  },
  copy: {
    flex: 1,
    minWidth: 0,
  },
  action: {
    minHeight: 32,
    paddingHorizontal: 12,
    borderRadius: Radii.pill,
    alignItems: "center",
    justifyContent: "center",
  },
});
