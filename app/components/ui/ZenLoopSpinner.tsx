import React, { useEffect, useRef } from "react";
import {
  Animated,
  Easing,
  StyleSheet,
  View,
} from "react-native";
import { useAppColors } from "../../constants/tokens";
import { resolveZenLogoDetailTint } from "./zenLogoPresentation";

const sageRing = require("../../assets/branding/zen-logo-ring-sage.png");
const ivoryRing = require("../../assets/branding/zen-logo-ring-ivory.png");

export function ZenLoopSpinner({ size = 24 }: { size?: number }) {
  const colors = useAppColors();
  const detailTint = resolveZenLogoDetailTint(colors);
  const orbitProgress = useRef(new Animated.Value(0)).current;
  const sageProgress = useRef(new Animated.Value(0)).current;
  const ivoryProgress = useRef(new Animated.Value(0)).current;

  useEffect(() => {
    orbitProgress.setValue(0);
    sageProgress.setValue(0);
    ivoryProgress.setValue(0);
    const orbitLoop = Animated.loop(
      Animated.timing(orbitProgress, {
        toValue: 1,
        duration: 3200,
        easing: Easing.linear,
        useNativeDriver: true,
      }),
    );
    const sageLoop = Animated.loop(
      Animated.timing(sageProgress, {
        toValue: 1,
        duration: 2600,
        easing: Easing.linear,
        useNativeDriver: true,
      }),
    );
    const ivoryLoop = Animated.loop(
      Animated.timing(ivoryProgress, {
        toValue: 1,
        duration: 3200,
        easing: Easing.linear,
        useNativeDriver: true,
      }),
    );
    orbitLoop.start();
    sageLoop.start();
    ivoryLoop.start();
    return () => {
      orbitLoop.stop();
      sageLoop.stop();
      ivoryLoop.stop();
      orbitProgress.stopAnimation();
      sageProgress.stopAnimation();
      ivoryProgress.stopAnimation();
    };
  }, [ivoryProgress, orbitProgress, sageProgress]);

  const orbitRotate = orbitProgress.interpolate({
    inputRange: [0, 1],
    outputRange: ["0deg", "360deg"],
  });

  const sageRotate = sageProgress.interpolate({
    inputRange: [0, 0.5, 1],
    outputRange: ["-8deg", "12deg", "-8deg"],
  });
  const ivoryRotate = ivoryProgress.interpolate({
    inputRange: [0, 0.5, 1],
    outputRange: ["8deg", "-12deg", "8deg"],
  });
  const sageScaleX = sageProgress.interpolate({
    inputRange: [0, 0.5, 1],
    outputRange: [0.96, 1.04, 0.96],
  });
  const sageScaleY = sageProgress.interpolate({
    inputRange: [0, 0.5, 1],
    outputRange: [1.04, 0.96, 1.04],
  });
  const ivoryScaleX = ivoryProgress.interpolate({
    inputRange: [0, 0.5, 1],
    outputRange: [1.03, 0.97, 1.03],
  });
  const ivoryScaleY = ivoryProgress.interpolate({
    inputRange: [0, 0.5, 1],
    outputRange: [0.97, 1.03, 0.97],
  });

  return (
    <View
      accessible={false}
      importantForAccessibility="no-hide-descendants"
      pointerEvents="none"
      style={[styles.root, { width: size, height: size }]}
    >
      <Animated.View
        style={[
          styles.orbit,
          {
            width: size,
            height: size,
            transform: [{ rotate: orbitRotate }],
          },
        ]}
      >
        <Animated.Image
          source={sageRing}
          resizeMode="contain"
          style={[
            styles.ring,
            {
              width: size,
              height: size,
              transform: [
                { rotate: sageRotate },
                { scaleX: sageScaleX },
                { scaleY: sageScaleY },
              ],
            },
          ]}
        />
        <Animated.Image
          source={ivoryRing}
          resizeMode="contain"
          style={[
            styles.ring,
            detailTint ? { tintColor: detailTint } : null,
            {
              width: size,
              height: size,
              transform: [
                { rotate: ivoryRotate },
                { scaleX: ivoryScaleX },
                { scaleY: ivoryScaleY },
              ],
            },
          ]}
        />
      </Animated.View>
    </View>
  );
}

const styles = StyleSheet.create({
  root: {
    alignItems: "center",
    justifyContent: "center",
  },
  orbit: {
    alignItems: "center",
    justifyContent: "center",
  },
  ring: {
    position: "absolute",
  },
});
