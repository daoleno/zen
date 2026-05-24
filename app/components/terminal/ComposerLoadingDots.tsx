import React, {
  useEffect,
  useRef,
} from "react";
import {
  Animated,
  Easing,
  StyleSheet,
  View,
} from "react-native";

interface ComposerLoadingDotsProps {
  color: string;
  size?: number;
  gap?: number;
}

const DOT_PHASES = [
  [0, 0.05, 0.22, 0.38, 1],
  [0, 0.29, 0.46, 0.62, 1],
  [0, 0.53, 0.7, 0.86, 1],
];

export function ComposerLoadingDots({
  color,
  size = 4,
  gap = 2.5,
}: ComposerLoadingDotsProps) {
  const progress = useRef(new Animated.Value(0)).current;

  useEffect(() => {
    progress.setValue(0);
    const animation = Animated.loop(
      Animated.sequence([
        Animated.timing(progress, {
          toValue: 1,
          duration: 1080,
          easing: Easing.out(Easing.cubic),
          useNativeDriver: true,
        }),
        Animated.timing(progress, {
          toValue: 0,
          duration: 0,
          useNativeDriver: true,
        }),
      ]),
    );
    animation.start();
    return () => {
      animation.stop();
      progress.stopAnimation();
      progress.setValue(0);
    };
  }, [progress]);

  return (
    <View
      accessible={false}
      importantForAccessibility="no-hide-descendants"
      pointerEvents="none"
      style={[
        styles.wrap,
        {
          gap,
          height: size + 5,
          width: size * 3 + gap * 2,
        },
      ]}
    >
      {DOT_PHASES.map((inputRange, index) => {
        const opacity = progress.interpolate({
          inputRange,
          outputRange: [0.42, 0.42, 1, 0.42, 0.42],
        });
        const scale = progress.interpolate({
          inputRange,
          outputRange: [0.72, 0.72, 1, 0.72, 0.72],
        });

        return (
          <Animated.View
            key={index}
            style={[
              styles.dot,
              {
                borderRadius: size / 2,
                backgroundColor: color,
                height: size,
                opacity,
                transform: [{ scale }],
                width: size,
              },
            ]}
          />
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: {
    alignItems: "center",
    flexDirection: "row",
    justifyContent: "center",
  },
  dot: {
    flexShrink: 0,
  },
});
