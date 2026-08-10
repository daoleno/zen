import React, { memo } from "react";
import { StyleSheet, View } from "react-native";
import Animated, {
  type SharedValue,
  useAnimatedStyle,
  useReducedMotion,
} from "react-native-reanimated";
import { Typography } from "../../constants/tokens";

type WorkSignalPullPreviewProps = {
  pullDistance: SharedValue<number>;
  threshold: number;
};

function WorkSignalPullPreviewComponent({
  pullDistance,
  threshold,
}: WorkSignalPullPreviewProps) {
  const reducedMotion = useReducedMotion();
  const rootStyle = useAnimatedStyle(() => {
    if (pullDistance.value <= 1) return { height: 0, opacity: 0 };
    const progress = Math.min(Math.max(pullDistance.value / threshold, 0), 1);
    return {
      height: Math.min(300, 72 + pullDistance.value * 1.05),
      opacity: Math.min(1, 0.2 + progress * 0.8),
    };
  });
  const revealStyle = useAnimatedStyle(() => {
    const progress = Math.min(Math.max(pullDistance.value / threshold, 0), 1);
    return {
      opacity: 0.5 + progress * 0.5,
      transform: reducedMotion ? [] : [{ translateY: (1 - progress) * 6 }],
    };
  });
  const pullTitleStyle = useAnimatedStyle(() => ({
    opacity: pullDistance.value < threshold ? 1 : 0,
  }));
  const releaseTitleStyle = useAnimatedStyle(() => ({
    opacity: pullDistance.value >= threshold ? 1 : 0,
  }));
  const trackStyle = useAnimatedStyle(() => ({
    width: `${Math.min(Math.max(pullDistance.value / threshold, 0), 1) * 100}%`,
  }));

  return (
    <Animated.View pointerEvents="none" style={[styles.root, rootStyle]}>
      <View style={styles.backdrop}>
        <View style={styles.content}>
          <Animated.View style={[styles.constellation, revealStyle]}>
            <View style={styles.line} />
            <View style={styles.dot} />
            <View style={styles.core}>
              <View style={styles.coreDot} />
            </View>
            <View style={styles.dot} />
          </Animated.View>
          <View style={styles.titleWrap}>
            <Animated.Text style={[styles.title, pullTitleStyle]}>
              Work in motion
            </Animated.Text>
            <Animated.Text
              style={[styles.title, styles.releaseTitle, releaseTitleStyle]}
            >
              Release to view Work
            </Animated.Text>
          </View>
          <Animated.Text style={[styles.hint, revealStyle]}>
            Active · Waiting · Ready
          </Animated.Text>
          <View style={styles.track}>
            <Animated.View style={[styles.trackFill, trackStyle]} />
          </View>
        </View>
      </View>
    </Animated.View>
  );
}

export const WorkSignalPullPreview = memo(WorkSignalPullPreviewComponent);

const styles = StyleSheet.create({
  root: {
    position: "absolute",
    top: 0,
    left: 0,
    right: 0,
    zIndex: 1,
    overflow: "hidden",
  },
  backdrop: {
    flex: 1,
    justifyContent: "flex-end",
    backgroundColor: "rgba(5, 9, 8, 0.96)",
  },
  content: { alignItems: "center", paddingHorizontal: 24, paddingBottom: 22 },
  constellation: {
    width: 88,
    height: 28,
    marginBottom: 10,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
  },
  line: {
    position: "absolute",
    left: 8,
    right: 8,
    height: 1,
    backgroundColor: "rgba(211, 226, 218, 0.28)",
  },
  dot: { width: 6, height: 6, borderRadius: 3, backgroundColor: "#A9C9B8" },
  core: {
    width: 26,
    height: 26,
    borderRadius: 13,
    alignItems: "center",
    justifyContent: "center",
    borderWidth: 1,
    borderColor: "rgba(169, 201, 184, 0.72)",
    backgroundColor: "rgba(169, 201, 184, 0.14)",
  },
  coreDot: { width: 8, height: 8, borderRadius: 4, backgroundColor: "#C7DED2" },
  titleWrap: { minHeight: 20, minWidth: 190, alignItems: "center" },
  title: {
    color: "#F4F1E8",
    fontSize: 14,
    lineHeight: 20,
    fontFamily: Typography.uiFontMedium,
  },
  releaseTitle: { position: "absolute" },
  hint: {
    marginTop: 4,
    color: "rgba(244, 241, 232, 0.58)",
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: 0.5,
  },
  track: {
    width: 116,
    height: 3,
    marginTop: 10,
    borderRadius: 2,
    overflow: "hidden",
    backgroundColor: "rgba(255, 255, 255, 0.16)",
  },
  trackFill: { height: 3, borderRadius: 2, backgroundColor: "#C7DED2" },
});
