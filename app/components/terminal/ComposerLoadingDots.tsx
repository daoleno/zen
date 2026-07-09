import React, { useEffect, useRef } from "react";
import {
  Animated,
  Easing,
  StyleSheet,
  View,
} from "react-native";

interface ComposerLoadingDotsProps {
  color: string;
  /** Core orb diameter. Halo extends beyond this. */
  size?: number;
  /** Kept for call-site compatibility; unused for the orb. */
  gap?: number;
}

/**
 * Perplexity-like thinking indicator: a soft breathing orb with an
 * out-of-phase halo. Prefer this over ActivityIndicator / bounce dots
 * for chat and composer waiting states.
 */
export function ComposerLoadingDots({
  color,
  size = 10,
}: ComposerLoadingDotsProps) {
  const core = useRef(new Animated.Value(0)).current;
  const halo = useRef(new Animated.Value(0)).current;
  const footprint = Math.max(size * 2.2, size + 8);

  useEffect(() => {
    core.setValue(0);
    halo.setValue(0);

    const coreLoop = Animated.loop(
      Animated.sequence([
        Animated.timing(core, {
          toValue: 1,
          duration: 980,
          easing: Easing.inOut(Easing.sin),
          useNativeDriver: true,
        }),
        Animated.timing(core, {
          toValue: 0,
          duration: 980,
          easing: Easing.inOut(Easing.sin),
          useNativeDriver: true,
        }),
      ]),
    );

    const haloLoop = Animated.loop(
      Animated.sequence([
        Animated.timing(halo, {
          toValue: 1,
          duration: 1280,
          easing: Easing.inOut(Easing.sin),
          useNativeDriver: true,
        }),
        Animated.timing(halo, {
          toValue: 0,
          duration: 1280,
          easing: Easing.inOut(Easing.sin),
          useNativeDriver: true,
        }),
      ]),
    );

    coreLoop.start();
    const haloDelay = setTimeout(() => {
      haloLoop.start();
    }, 180);

    return () => {
      clearTimeout(haloDelay);
      coreLoop.stop();
      haloLoop.stop();
      core.stopAnimation();
      halo.stopAnimation();
      core.setValue(0);
      halo.setValue(0);
    };
  }, [core, halo]);

  const coreOpacity = core.interpolate({
    inputRange: [0, 1],
    outputRange: [0.42, 1],
  });
  const coreScale = core.interpolate({
    inputRange: [0, 1],
    outputRange: [0.86, 1],
  });
  const haloOpacity = halo.interpolate({
    inputRange: [0, 1],
    outputRange: [0.1, 0.28],
  });
  const haloScale = halo.interpolate({
    inputRange: [0, 1],
    outputRange: [1, 1.55],
  });

  return (
    <View
      accessible={false}
      importantForAccessibility="no-hide-descendants"
      pointerEvents="none"
      style={[
        styles.wrap,
        {
          height: footprint,
          width: footprint,
        },
      ]}
    >
      <Animated.View
        style={[
          styles.orb,
          {
            backgroundColor: color,
            borderRadius: size,
            height: size * 1.7,
            opacity: haloOpacity,
            transform: [{ scale: haloScale }],
            width: size * 1.7,
          },
        ]}
      />
      <Animated.View
        style={[
          styles.orb,
          styles.core,
          {
            backgroundColor: color,
            borderRadius: size / 2,
            height: size,
            opacity: coreOpacity,
            transform: [{ scale: coreScale }],
            width: size,
          },
        ]}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: {
    alignItems: "center",
    justifyContent: "center",
  },
  orb: {
    position: "absolute",
  },
  core: {
    // Keep the solid core above the soft halo.
    zIndex: 1,
  },
});
