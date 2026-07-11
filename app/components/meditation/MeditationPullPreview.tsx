import React, { memo } from "react";
import {
  StyleSheet,
  View,
} from "react-native";
import { LinearGradient } from "expo-linear-gradient";
import Animated, {
  useAnimatedStyle,
  type SharedValue,
} from "react-native-reanimated";
import { Typography } from "../../constants/tokens";

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
  const marksStyle = useAnimatedStyle(() => {
    const progress = Math.max(
      0,
      Math.min(pullDistance.value / threshold, 1),
    );
    return {
      opacity: 0.55 + progress * 0.45,
      transform: [{ translateY: (1 - progress) * 8 }],
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
      <LinearGradient
        colors={[
          "rgba(6,10,9,0.42)",
          "rgba(5,9,8,0.72)",
          "rgba(4,7,7,0.94)",
        ]}
        locations={[0, 0.48, 1]}
        style={styles.image}
      >
        <View style={styles.content}>
          <Animated.View style={[styles.marks, marksStyle]}>
            <View style={styles.markBreath}>
              <View style={styles.markBreathOuter} />
              <View style={styles.markBreathInner} />
            </View>
            <View style={styles.markWood} />
            <View style={styles.markWindow}>
              <View style={styles.markWindowScene} />
            </View>
          </Animated.View>
          <View style={styles.titleWrap}>
            <Animated.Text style={[styles.title, pullTitleStyle]}>
              Quiet Mode
            </Animated.Text>
            <Animated.Text
              style={[styles.title, styles.releaseTitle, releaseTitleStyle]}
            >
              Release into Quiet Mode
            </Animated.Text>
          </View>
          <Animated.Text style={[styles.hint, marksStyle]}>
            冥想 · 木鱼 · 世界之窗
          </Animated.Text>
          <View style={styles.track}>
            <Animated.View style={[styles.trackFill, trackFillStyle]} />
          </View>
        </View>
      </LinearGradient>
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
  marks: {
    flexDirection: "row",
    alignItems: "center",
    gap: 20,
    marginBottom: 12,
    minHeight: 28,
  },
  markBreath: {
    width: 26,
    height: 20,
    alignItems: "center",
    justifyContent: "center",
  },
  markBreathOuter: {
    position: "absolute",
    width: 26,
    height: 18,
    borderRadius: 13,
    borderWidth: 1,
    borderColor: "rgba(214,224,208,0.55)",
  },
  markBreathInner: {
    width: 10,
    height: 7,
    borderRadius: 5,
    backgroundColor: "rgba(236,236,228,0.42)",
  },
  markWood: {
    width: 28,
    height: 18,
    borderRadius: 10,
    backgroundColor: "rgba(74,40,20,0.88)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(212,165,116,0.45)",
  },
  markWindow: {
    width: 28,
    height: 20,
    borderRadius: 2,
    padding: 2.5,
    backgroundColor: "rgba(20,24,28,0.92)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(180,190,200,0.35)",
  },
  markWindowScene: {
    flex: 1,
    borderRadius: 1,
    backgroundColor: "rgba(96,128,118,0.72)",
  },
  titleWrap: {
    minHeight: 19,
    minWidth: 180,
    alignItems: "center",
  },
  title: {
    color: "#F4F1E8",
    fontSize: 14,
    lineHeight: 19,
    fontFamily: Typography.uiFontMedium,
  },
  releaseTitle: {
    position: "absolute",
  },
  hint: {
    marginTop: 6,
    color: "rgba(244,241,232,0.58)",
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: 0.8,
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
