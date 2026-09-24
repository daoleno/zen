import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  View,
  type StyleProp,
  type ViewStyle,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import Svg, { Circle, Defs, RadialGradient, Stop } from "react-native-svg";
import { useAppTheme } from "../../constants/tokens";
import { AppText } from "./AppText";
import { Button } from "./Button";
import { Enter } from "./Enter";

type Icon = React.ComponentProps<typeof Ionicons>["name"];

export interface EmptyAction {
  label: string;
  icon?: Icon;
  onPress(): void;
  disabled?: boolean;
  loading?: boolean;
}

interface EmptyStateProps {
  title?: string;
  detail?: string | null;
  icon?: Icon;
  busy?: boolean;
  tone?: "default" | "danger";
  action?: EmptyAction;
  secondary?: EmptyAction;
  /** `inline` sits inside lists and sheets: no halo, smaller type. */
  size?: "hero" | "inline";
  style?: StyleProp<ViewStyle>;
}

const HALO = 88;

/** Shared empty, loading and error state for screens, lists and sheets. */
export function EmptyState({
  title,
  detail,
  icon,
  busy = false,
  tone = "default",
  action,
  secondary,
  size = "hero",
  style,
}: EmptyStateProps) {
  const { colors } = useAppTheme();
  const danger = tone === "danger";
  const glyphColor = danger ? colors.dangerText : colors.accentStrong;

  if (size === "inline") {
    return (
      <View style={[styles.inline, style]} accessibilityLiveRegion="polite">
        {busy ? <ActivityIndicator color={colors.textSecondary} /> : null}
        {title ? (
          <AppText variant="label" tone={danger ? "danger" : "secondary"} style={styles.center}>
            {title}
          </AppText>
        ) : null}
        {detail ? (
          <AppText variant="caption" tone={danger ? "danger" : "tertiary"} style={styles.center}>
            {detail}
          </AppText>
        ) : null}
        {action ? (
          <Button size="sm" variant="tinted" {...actionProps(action)} style={styles.inlineAction} />
        ) : null}
      </View>
    );
  }

  return (
    <Enter preset="fade" style={[styles.hero, style]}>
      <View style={styles.halo} accessible={false} importantForAccessibility="no-hide-descendants">
        <Svg width={HALO} height={HALO} style={StyleSheet.absoluteFill}>
          <Defs>
            <RadialGradient id="halo" cx="50%" cy="42%" r="58%">
              <Stop offset="0" stopColor={glyphColor} stopOpacity={0.2} />
              <Stop offset="0.72" stopColor={glyphColor} stopOpacity={0.06} />
              <Stop offset="1" stopColor={glyphColor} stopOpacity={0} />
            </RadialGradient>
          </Defs>
          <Circle cx={HALO / 2} cy={HALO / 2} r={HALO / 2} fill="url(#halo)" />
          <Circle
            cx={HALO / 2}
            cy={HALO / 2}
            r={HALO / 2 - 14}
            fill="none"
            stroke={glyphColor}
            strokeOpacity={0.18}
            strokeWidth={1}
          />
        </Svg>
        {busy ? (
          <ActivityIndicator color={glyphColor} />
        ) : icon ? (
          <Ionicons name={icon} size={30} color={glyphColor} />
        ) : null}
      </View>
      {title ? (
        <AppText
          variant="title"
          accessibilityRole="header"
          accessibilityLiveRegion="polite"
          style={[styles.center, styles.title]}
        >
          {title}
        </AppText>
      ) : null}
      {detail ? (
        <AppText variant="body" tone={danger ? "danger" : "secondary"} style={[styles.center, styles.detail]}>
          {detail}
        </AppText>
      ) : null}
      {action || secondary ? (
        <View style={styles.actions}>
          {action ? <Button variant="filled" {...actionProps(action)} /> : null}
          {secondary ? <Button variant="plain" {...actionProps(secondary)} /> : null}
        </View>
      ) : null}
    </Enter>
  );
}

function actionProps(action: EmptyAction) {
  return {
    label: action.label,
    icon: action.icon,
    onPress: action.onPress,
    disabled: action.disabled,
    loading: action.loading,
  };
}

const styles = StyleSheet.create({
  hero: {
    width: "100%",
    maxWidth: 420,
    alignSelf: "center",
    paddingHorizontal: 28,
    paddingVertical: 32,
    alignItems: "center",
  },
  halo: {
    width: HALO,
    height: HALO,
    alignItems: "center",
    justifyContent: "center",
    marginBottom: 18,
  },
  title: {
    marginBottom: 6,
  },
  detail: {
    maxWidth: 320,
  },
  center: {
    textAlign: "center",
  },
  actions: {
    marginTop: 22,
    alignItems: "center",
    gap: 6,
  },
  inline: {
    minHeight: 120,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 18,
    gap: 6,
  },
  inlineAction: {
    marginTop: 8,
  },
});
