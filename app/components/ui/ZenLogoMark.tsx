import React from "react";
import {
  Image,
  StyleSheet,
  View,
  type AccessibilityProps,
  type StyleProp,
  type ViewStyle,
} from "react-native";
import { useAppColors } from "../../constants/tokens";
import { resolveZenLogoDetailTint } from "./zenLogoPresentation";

const mark = require("../../assets/branding/zen-logo-mark-transparent.png");
const detailLayer = require("../../assets/branding/zen-logo-ring-ivory.png");

interface ZenLogoMarkProps
  extends Pick<
    AccessibilityProps,
    "accessible" | "accessibilityLabel" | "accessibilityIgnoresInvertColors"
  > {
  size: number;
  style?: StyleProp<ViewStyle>;
}

export function ZenLogoMark({
  size,
  style,
  accessible,
  accessibilityLabel,
  accessibilityIgnoresInvertColors,
}: ZenLogoMarkProps) {
  const colors = useAppColors();
  const detailTint = resolveZenLogoDetailTint(colors);

  return (
    <View
      accessible={accessible}
      accessibilityLabel={accessibilityLabel}
      style={[styles.root, { width: size, height: size }, style]}
    >
      <Image
        source={mark}
        resizeMode="contain"
        accessible={false}
        accessibilityIgnoresInvertColors={accessibilityIgnoresInvertColors}
        style={styles.layer}
      />
      {detailTint ? (
        <Image
          source={detailLayer}
          resizeMode="contain"
          accessible={false}
          accessibilityIgnoresInvertColors={accessibilityIgnoresInvertColors}
          style={[styles.layer, { tintColor: detailTint }]}
        />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  root: {
    position: "relative",
  },
  layer: {
    position: "absolute",
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
    width: "100%",
    height: "100%",
  },
});
