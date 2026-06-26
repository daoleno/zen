import React, { useMemo } from "react";
import { Pressable, type PressableProps, type StyleProp, type ViewStyle } from "react-native";
import Animated, {
  useAnimatedStyle,
  useSharedValue,
  withSpring,
  interpolate,
} from "react-native-reanimated";
import { Spring, PRESSED_SCALE } from "../../constants/motion";

const ReanimatedPressable = Animated.createAnimatedComponent(Pressable);

export interface AnimatedPressableProps extends Omit<PressableProps, "style"> {
  style?: StyleProp<ViewStyle>;
  /** Press scale factor. Defaults to 0.96. Set to 1 to disable scale (keep opacity only). */
  scale?: number;
  /** Spring preset name. Defaults to "press". */
  preset?: keyof typeof Spring;
}

/**
 * A Pressable that responds to touch with a Reanimated spring — a subtle
 * scale-down on press-in and a calm spring-back on release. This is the single
 * tappable surface used app-wide so every interaction feels like the same
 * material. Runs on the UI thread for frame-accurate 60fps feedback.
 */
export const AnimatedPressable = React.forwardRef<
  React.ElementRef<typeof Pressable>,
  AnimatedPressableProps
>(function AnimatedPressable(
  {
    children,
    style,
    scale = PRESSED_SCALE,
    preset = "press",
    onPressIn,
    onPressOut,
    ...props
  },
  ref,
) {
  const pressed = useSharedValue(0);
  const config = useMemo(() => Spring[preset], [preset]);

  const animatedStyle = useAnimatedStyle(() => {
    const s = interpolate(pressed.value, [0, 1], [1, scale]);
    return { transform: [{ scale: s }] };
  });

  return (
    <ReanimatedPressable
      ref={ref}
      {...props}
      onPressIn={(event) => {
        pressed.value = withSpring(1, config);
        onPressIn?.(event);
      }}
      onPressOut={(event) => {
        pressed.value = withSpring(0, config);
        onPressOut?.(event);
      }}
      style={[{ opacity: props.disabled ? 0.5 : 1 }, style, animatedStyle]}
    >
      {children}
    </ReanimatedPressable>
  );
});

export { Animated };
