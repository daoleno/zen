import React from "react";
import {
  StyleSheet,
  View,
  type ColorValue,
  type StyleProp,
  type ViewStyle,
} from "react-native";
import { LinearGradient } from "expo-linear-gradient";
import { useAppTheme } from "../../constants/tokens";

type SkyNatureBackdropProps = {
  height?: number;
  fill?: boolean;
  style?: StyleProp<ViewStyle>;
};

export function SkyNatureBackdrop({
  height = 620,
  fill = false,
  style,
}: SkyNatureBackdropProps) {
  const { colors, isLight } = useAppTheme();
  const sky: readonly [ColorValue, ColorValue, ColorValue, ColorValue] = isLight
    ? ["#489FFC", "#50AEFE", "#82CCFC", "#6A8E38"]
    : ["#000212", "#021332", "#08234C", "#02040A"];
  const veil: readonly [ColorValue, ColorValue, ColorValue] = isLight
    ? ["rgba(246,248,251,0.00)", "rgba(246,248,251,0.12)", colors.bgPrimary]
    : ["rgba(14,17,22,0.00)", "rgba(14,17,22,0.18)", colors.bgPrimary];

  return (
    <View
      pointerEvents="none"
      style={[
        styles.root,
        fill
          ? { top: 0, bottom: 0, backgroundColor: colors.bgPrimary }
          : { height, backgroundColor: colors.bgPrimary },
        style,
      ]}
    >
      <LinearGradient
        colors={sky}
        locations={[0, 0.34, 0.66, 1]}
        style={styles.image}
      >
        <View
          style={[
            StyleSheet.absoluteFill,
            {
              backgroundColor: isLight
                ? "rgba(46,124,255,0.03)"
                : "rgba(76,141,255,0.08)",
            },
          ]}
        />
        <LinearGradient
          pointerEvents="none"
          colors={veil}
          locations={[0, 0.74, 1]}
          style={StyleSheet.absoluteFill}
        />
      </LinearGradient>
    </View>
  );
}

const styles = StyleSheet.create({
  root: {
    position: "absolute",
    top: 0,
    left: 0,
    right: 0,
    overflow: "hidden",
  },
  image: {
    flex: 1,
  },
});
