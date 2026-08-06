import React, { useEffect } from "react";
import { StyleSheet, View } from "react-native";
import Svg, { Circle } from "react-native-svg";
import Reanimated, {
  Easing,
  ReduceMotion,
  cancelAnimation,
  useAnimatedStyle,
  useReducedMotion,
  useSharedValue,
  withRepeat,
  withTiming,
} from "react-native-reanimated";
import {
  PENDING_SEND_CLOCK_FAST_HAND_REST_DEG,
  PENDING_SEND_CLOCK_FAST_PERIOD_MS,
  PENDING_SEND_CLOCK_HAND_STROKE,
  PENDING_SEND_CLOCK_SLOW_HAND_REST_DEG,
  PENDING_SEND_CLOCK_SLOW_PERIOD_MS,
  PENDING_SEND_STATUS_MARK_SIZE,
} from "./pendingSendStatusGeometry";

type PendingSendStatusMarkProps = {
  color: string;
  size?: number;
};

/**
 * Compact outbound sending clock as an absolute child of the user bubble.
 * Two UI-thread hands match MsgClockDrawable: bars start at the dial center
 * (not mid-line through it), fast rests vertical, slow rests horizontal-right
 * at 3× period. Reduced motion freezes both rests. No JS timers.
 */
export function PendingSendStatusMark({
  color,
  size = PENDING_SEND_STATUS_MARK_SIZE,
}: PendingSendStatusMarkProps) {
  const reducedMotion = useReducedMotion();
  const fastRotation = useSharedValue(PENDING_SEND_CLOCK_FAST_HAND_REST_DEG);
  const slowRotation = useSharedValue(PENDING_SEND_CLOCK_SLOW_HAND_REST_DEG);

  useEffect(() => {
    if (reducedMotion) {
      cancelAnimation(fastRotation);
      cancelAnimation(slowRotation);
      fastRotation.value = PENDING_SEND_CLOCK_FAST_HAND_REST_DEG;
      slowRotation.value = PENDING_SEND_CLOCK_SLOW_HAND_REST_DEG;
      return;
    }
    fastRotation.value = PENDING_SEND_CLOCK_FAST_HAND_REST_DEG;
    slowRotation.value = PENDING_SEND_CLOCK_SLOW_HAND_REST_DEG;
    fastRotation.value = withRepeat(
      withTiming(PENDING_SEND_CLOCK_FAST_HAND_REST_DEG + 360, {
        duration: PENDING_SEND_CLOCK_FAST_PERIOD_MS,
        easing: Easing.linear,
        reduceMotion: ReduceMotion.System,
      }),
      -1,
      false,
      undefined,
      ReduceMotion.System,
    );
    slowRotation.value = withRepeat(
      withTiming(PENDING_SEND_CLOCK_SLOW_HAND_REST_DEG + 360, {
        duration: PENDING_SEND_CLOCK_SLOW_PERIOD_MS,
        easing: Easing.linear,
        reduceMotion: ReduceMotion.System,
      }),
      -1,
      false,
      undefined,
      ReduceMotion.System,
    );
    return () => {
      cancelAnimation(fastRotation);
      cancelAnimation(slowRotation);
    };
  }, [fastRotation, reducedMotion, slowRotation]);

  const fastHandStyle = useAnimatedStyle(() => ({
    transform: [{ rotate: `${fastRotation.value}deg` }],
  }));
  const slowHandStyle = useAnimatedStyle(() => ({
    transform: [{ rotate: `${slowRotation.value}deg` }],
  }));

  const center = size / 2;
  const radius = size / 2 - 0.75;
  // MsgClockDrawable proportions at ~12dp: long ≈3dp, short ≈2.3dp.
  const fastHandLength = radius * 0.68;
  const slowHandLength = radius * 0.52;
  // Pivot: bar bottom at center, left at center - stroke/2 → rest 0° is
  // center→12 and rest 90° is center→3, matching canvas.drawLine(center,…).
  const handLeft = center - PENDING_SEND_CLOCK_HAND_STROKE / 2;

  return (
    <View
      accessible={false}
      importantForAccessibility="no"
      pointerEvents="none"
      style={{ width: size, height: size }}
    >
      <Svg width={size} height={size} style={StyleSheet.absoluteFill}>
        <Circle
          cx={center}
          cy={center}
          r={radius}
          stroke={color}
          strokeWidth={PENDING_SEND_CLOCK_HAND_STROKE}
          fill="none"
        />
      </Svg>
      <Reanimated.View
        style={[styles.handLayer, { width: size, height: size }, slowHandStyle]}
      >
        <View
          style={[
            styles.hand,
            {
              backgroundColor: color,
              width: PENDING_SEND_CLOCK_HAND_STROKE,
              height: slowHandLength,
              left: handLeft,
              bottom: center,
            },
          ]}
        />
      </Reanimated.View>
      <Reanimated.View
        style={[styles.handLayer, { width: size, height: size }, fastHandStyle]}
      >
        <View
          style={[
            styles.hand,
            {
              backgroundColor: color,
              width: PENDING_SEND_CLOCK_HAND_STROKE,
              height: fastHandLength,
              left: handLeft,
              bottom: center,
            },
          ]}
        />
      </Reanimated.View>
    </View>
  );
}

const styles = StyleSheet.create({
  handLayer: {
    ...StyleSheet.absoluteFill,
  },
  hand: {
    position: "absolute",
    borderRadius: 1,
  },
});
