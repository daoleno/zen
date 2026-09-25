import React, { useEffect } from "react";
import { StyleSheet, View, type StyleProp, type ViewStyle } from "react-native";
import Animated, {
  cancelAnimation,
  useAnimatedStyle,
  useSharedValue,
  withRepeat,
  withTiming,
} from "react-native-reanimated";
import { useAppTheme } from "../../constants/tokens";
import { AppText } from "./AppText";

export type StatusTone = "success" | "warning" | "danger" | "accent" | "neutral";

interface StatusPillProps {
  label: string;
  tone?: StatusTone;
  /** Breathing dot for live states such as Connecting. */
  live?: boolean;
  style?: StyleProp<ViewStyle>;
}

export function StatusPill({ label, tone = "neutral", live = false, style }: StatusPillProps) {
  const { colors, theme } = useAppTheme();
  const palette = {
    success: { fill: colors.successSoft, ink: colors.success },
    warning: { fill: colors.warningSoft, ink: colors.warning },
    danger: { fill: colors.dangerSoft, ink: colors.dangerText },
    accent: { fill: theme.materials.tint, ink: colors.accentStrong },
    neutral: { fill: colors.surfaceSubtle, ink: colors.textSecondary },
  }[tone];
  const pulse = useSharedValue(1);
  useEffect(() => {
    if (!live) {
      cancelAnimation(pulse);
      pulse.value = 1;
      return;
    }
    pulse.value = withRepeat(withTiming(0.35, { duration: 900 }), -1, true);
    return () => cancelAnimation(pulse);
  }, [live, pulse]);
  const dotStyle = useAnimatedStyle(() => ({ opacity: pulse.value }));

  return (
    <View style={[styles.pill, { backgroundColor: palette.fill }, style]} accessibilityLabel={label}>
      <Animated.View style={[styles.dot, { backgroundColor: palette.ink }, dotStyle]} />
      <AppText variant="micro" numberOfLines={1} style={{ color: palette.ink }}>
        {label}
      </AppText>
    </View>
  );
}

const styles = StyleSheet.create({
  pill: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    minHeight: 24,
    paddingHorizontal: 10,
    borderRadius: 999,
    alignSelf: "flex-start",
  },
  dot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
});
