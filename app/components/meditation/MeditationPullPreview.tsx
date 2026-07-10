import React, { memo } from "react";
import {
  ImageBackground,
  StyleSheet,
  View,
} from "react-native";
import { LinearGradient } from "expo-linear-gradient";
import Animated, {
  useAnimatedStyle,
  type SharedValue,
} from "react-native-reanimated";
import { Typography } from "../../constants/tokens";

const MEDITATION_BACKGROUND = require("../../assets/theme/meditation-sky-garden.webp");

type MeditationPullPreviewProps = {
  pullDistance: SharedValue<number>;
  threshold: number;
};

function MeditationPullPreviewComponent({
  pullDistance,
  threshold,
}: MeditationPullPreviewProps) {
  const rootStyle = useAnimatedStyle(() => {
    if (pullDistance.value <= 1) {
      return { height: 0, opacity: 0 };
    }
    const progress = Math.max(
      0,
      Math.min(pullDistance.value / threshold, 1),
    );
    return {
      height: Math.min(360, 86 + pullDistance.value * 1.18),
      opacity: Math.min(1, 0.18 + progress * 0.88),
    };
  });
  const haloStyle = useAnimatedStyle(() => {
    const progress = Math.max(
      0,
      Math.min(pullDistance.value / threshold, 1),
    );
    return {
      opacity: 0.08 + progress * 0.18,
      transform: [{ scale: 1 + progress * 0.42 }],
    };
  });
  const orbStyle = useAnimatedStyle(() => {
    const progress = Math.max(
      0,
      Math.min(pullDistance.value / threshold, 1),
    );
    return {
      opacity: 0.72 + progress * 0.28,
      transform: [{ scale: 0.74 + progress * 0.42 }],
    };
  });
  const pullTitleStyle = useAnimatedStyle(() => ({
    opacity: pullDistance.value < threshold ? 1 : 0,
  }));
  const releaseTitleStyle = useAnimatedStyle(() => ({
    opacity: pullDistance.value >= threshold ? 1 : 0,
  }));
  const trackFillStyle = useAnimatedStyle(() => {
    const progress = Math.max(
      0,
      Math.min(pullDistance.value / threshold, 1),
    );
    return { width: `${progress * 100}%` };
  });

  return (
    <Animated.View pointerEvents="none" style={[styles.root, rootStyle]}>
      <ImageBackground
        source={MEDITATION_BACKGROUND}
        resizeMode="cover"
        style={styles.image}
      >
        <LinearGradient
          colors={[
            "rgba(5,10,22,0.10)",
            "rgba(5,10,22,0.34)",
            "rgba(5,10,22,0.82)",
          ]}
          locations={[0, 0.52, 1]}
          style={StyleSheet.absoluteFill}
        />
        <View style={styles.content}>
          <View style={styles.orbWrap}>
            <Animated.View style={[styles.orbHalo, haloStyle]} />
            <Animated.View style={[styles.orb, orbStyle]}>
              <LinearGradient
                colors={[
                  "rgba(255,255,255,0.92)",
                  "rgba(107,176,255,0.44)",
                  "rgba(44,95,204,0.18)",
                ]}
                style={StyleSheet.absoluteFill}
              />
            </Animated.View>
          </View>
          <View style={styles.titleWrap}>
            <Animated.Text style={[styles.title, pullTitleStyle]}>
              Pull into quiet
            </Animated.Text>
            <Animated.Text
              style={[styles.title, styles.releaseTitle, releaseTitleStyle]}
            >
              Release into quiet
            </Animated.Text>
          </View>
          <View style={styles.track}>
            <Animated.View style={[styles.trackFill, trackFillStyle]} />
          </View>
        </View>
      </ImageBackground>
    </Animated.View>
  );
}

export const MeditationPullPreview = memo(MeditationPullPreviewComponent);

const styles = StyleSheet.create({
  root: {
    position: "absolute",
    top: 0,
    left: 0,
    right: 0,
    zIndex: 1,
    overflow: "hidden",
  },
  image: {
    flex: 1,
    justifyContent: "flex-end",
  },
  content: {
    alignItems: "center",
    paddingHorizontal: 24,
    paddingBottom: 24,
  },
  orbWrap: {
    width: 92,
    height: 92,
    alignItems: "center",
    justifyContent: "center",
    marginBottom: 8,
  },
  orbHalo: {
    position: "absolute",
    width: 78,
    height: 78,
    borderRadius: 39,
    backgroundColor: "rgba(128,190,255,0.72)",
  },
  orb: {
    width: 62,
    height: 62,
    borderRadius: 31,
    overflow: "hidden",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.62)",
  },
  titleWrap: {
    minHeight: 19,
    minWidth: 140,
    alignItems: "center",
  },
  title: {
    color: "#F7FAFF",
    fontSize: 14,
    lineHeight: 19,
    fontFamily: Typography.uiFontMedium,
  },
  releaseTitle: {
    position: "absolute",
  },
  track: {
    width: 120,
    height: 3,
    borderRadius: 2,
    marginTop: 10,
    overflow: "hidden",
    backgroundColor: "rgba(255,255,255,0.18)",
  },
  trackFill: {
    height: 3,
    borderRadius: 2,
    backgroundColor: "rgba(255,255,255,0.82)",
  },
});
