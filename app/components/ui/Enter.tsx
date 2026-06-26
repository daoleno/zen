import React from "react";
import { type StyleProp, type ViewStyle } from "react-native";
import Animated, { FadeIn, FadeInDown, ZoomIn } from "react-native-reanimated";

export type EnterPreset = "fade" | "rise" | "pop";

interface FadeInProps {
  preset?: EnterPreset;
  delay?: number;
  style?: StyleProp<ViewStyle>;
  children: React.ReactNode;
}

/**
 * A wrapper that mounts its children with a gentle entrance — the shared
 * "something just appeared" motion used for empty states, list sections, and
 * first-paint content. Consistent across the app via Reanimated entering
 * presets.
 */
export function Enter({ preset = "rise", delay = 0, style, children }: FadeInProps) {
  switch (preset) {
    case "fade":
      return (
        <Animated.View entering={FadeIn.delay(delay).duration(300)} style={style}>
          {children}
        </Animated.View>
      );
    case "pop":
      return (
        <Animated.View
          entering={ZoomIn.delay(delay).duration(380).springify().damping(16)}
          style={style}
        >
          {children}
        </Animated.View>
      );
    case "rise":
    default:
      return (
        <Animated.View entering={FadeInDown.delay(delay).duration(420).springify().damping(22)} style={style}>
          {children}
        </Animated.View>
      );
  }
}
