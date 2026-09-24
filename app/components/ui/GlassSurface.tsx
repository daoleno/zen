import React from "react";
import {
  Platform,
  StyleSheet,
  View,
  type StyleProp,
  type ViewProps,
  type ViewStyle,
} from "react-native";
import { GlassView, isLiquidGlassAvailable } from "expo-glass-effect";
import { ContinuousCorners, shadow, useAppTheme } from "../../constants/tokens";
import type { MaterialPalette } from "../../theme";
import { useReduceTransparency } from "./useReduceTransparency";

export type GlassMaterial = keyof Pick<
  MaterialPalette,
  "chrome" | "regular" | "thick" | "thin"
>;

export interface GlassSurfaceProps extends ViewProps {
  material?: GlassMaterial;
  radius?: number;
  elevation?: "none" | "card" | "raised" | "float";
  /** Accent-tinted fill for selected or primary floating controls. */
  tinted?: boolean;
  /** iOS Liquid Glass reacts to touch. */
  interactive?: boolean;
  style?: StyleProp<ViewStyle>;
}

const LIQUID_GLASS = Platform.OS === "ios" && isLiquidGlassAvailable();

/**
 * The single material owner. iOS 26 renders system Liquid Glass; elsewhere a
 * translucent fill, an outer hairline and a lit top edge stand in for it.
 * Reduce Transparency switches to an opaque fill.
 */
export function GlassSurface({
  material = "regular",
  radius = 0,
  elevation = "none",
  tinted = false,
  interactive = false,
  style,
  children,
  ...viewProps
}: GlassSurfaceProps) {
  const { theme, colors } = useAppTheme();
  const reduceTransparency = useReduceTransparency();
  const materials = theme.materials;
  const lift =
    elevation === "none" ? null : shadow(elevation, colors.shadowColor);
  const shape: ViewStyle = { borderRadius: radius, ...ContinuousCorners };

  if (LIQUID_GLASS && !reduceTransparency) {
    return (
      <GlassView
        {...viewProps}
        glassEffectStyle={material === "thin" ? "clear" : "regular"}
        colorScheme={theme.colorScheme}
        tintColor={tinted ? colors.accent : undefined}
        isInteractive={interactive}
        style={[shape, lift, style]}
      >
        {children}
      </GlassView>
    );
  }

  const fill = reduceTransparency
    ? material === "thin"
      ? colors.bgElevated
      : colors.modalSurface
    : materials[material];

  return (
    <View
      {...viewProps}
      style={[
        shape,
        styles.frame,
        { backgroundColor: fill, borderColor: materials.stroke },
        lift,
        style,
      ]}
    >
      {tinted ? (
        <View
          pointerEvents="none"
          style={[StyleSheet.absoluteFill, shape, { backgroundColor: materials.tint }]}
        />
      ) : null}
      <View
        pointerEvents="none"
        style={[
          styles.highlight,
          {
            left: Math.max(radius * 0.6, 8),
            right: Math.max(radius * 0.6, 8),
            backgroundColor: materials.highlight,
          },
        ]}
      />
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  frame: {
    borderWidth: StyleSheet.hairlineWidth,
  },
  highlight: {
    position: "absolute",
    top: 0,
    height: StyleSheet.hairlineWidth,
  },
});
