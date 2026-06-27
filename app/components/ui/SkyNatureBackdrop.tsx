import React from "react";
import {
  ImageBackground,
  StyleSheet,
  View,
  type ColorValue,
  type ImageSourcePropType,
  type StyleProp,
  type ViewStyle,
} from "react-native";
import { LinearGradient } from "expo-linear-gradient";
import { useAppTheme } from "../../constants/tokens";

const SKY_MEADOW: ImageSourcePropType = require("../../assets/theme/sky-meadow-ambient.webp");
const MOONLIT_MEADOW: ImageSourcePropType = require("../../assets/theme/moonlit-meadow-ambient.webp");

type SkyNatureBackdropProps = {
  height?: number;
  style?: StyleProp<ViewStyle>;
};

export function SkyNatureBackdrop({
  height = 280,
  style,
}: SkyNatureBackdropProps) {
  const { colors, isLight } = useAppTheme();
  const overlay: readonly [ColorValue, ColorValue, ColorValue] = isLight
    ? ["rgba(246,248,251,0.00)", "rgba(246,248,251,0.20)", colors.bgPrimary]
    : ["rgba(14,17,22,0.04)", "rgba(14,17,22,0.34)", colors.bgPrimary];
  const source = isLight ? SKY_MEADOW : MOONLIT_MEADOW;

  return (
    <View
      pointerEvents="none"
      style={[styles.root, { height, backgroundColor: colors.bgPrimary }, style]}
    >
      <ImageBackground
        source={source}
        resizeMode="cover"
        style={styles.image}
        imageStyle={[styles.imageInner, { opacity: isLight ? 0.98 : 0.92 }]}
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
          colors={overlay}
          locations={[0, 0.62, 1]}
          style={StyleSheet.absoluteFill}
        />
      </ImageBackground>
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
  imageInner: {
    opacity: 1,
  },
});
