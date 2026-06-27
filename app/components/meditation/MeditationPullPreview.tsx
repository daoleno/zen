import React from "react";
import {
  ImageBackground,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { LinearGradient } from "expo-linear-gradient";
import { Typography } from "../../constants/tokens";

const MEDITATION_BACKGROUND = require("../../assets/theme/meditation-sky-garden.webp");

type MeditationPullPreviewProps = {
  progress: number;
  pullDistance: number;
};

export function MeditationPullPreview({
  progress,
  pullDistance,
}: MeditationPullPreviewProps) {
  if (pullDistance <= 1) {
    return null;
  }

  const clamped = Math.max(0, Math.min(progress, 1));
  const height = Math.min(360, 86 + pullDistance * 1.18);
  const orbScale = 0.74 + clamped * 0.42;
  const opacity = Math.min(1, 0.18 + clamped * 0.88);

  return (
    <View pointerEvents="none" style={[styles.root, { height, opacity }]}>
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
            <View
              style={[
                styles.orbHalo,
                {
                  opacity: 0.08 + clamped * 0.18,
                  transform: [{ scale: 1 + clamped * 0.42 }],
                },
              ]}
            />
            <View
              style={[
                styles.orb,
                {
                  opacity: 0.72 + clamped * 0.28,
                  transform: [{ scale: orbScale }],
                },
              ]}
            >
              <LinearGradient
                colors={[
                  "rgba(255,255,255,0.92)",
                  "rgba(107,176,255,0.44)",
                  "rgba(44,95,204,0.18)",
                ]}
                style={StyleSheet.absoluteFill}
              />
            </View>
          </View>
          <Text style={styles.title}>
            {clamped >= 1 ? "Release into quiet" : "Pull into quiet"}
          </Text>
          <View style={styles.track}>
            <View style={[styles.trackFill, { width: `${clamped * 100}%` }]} />
          </View>
        </View>
      </ImageBackground>
    </View>
  );
}

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
  title: {
    color: "#F7FAFF",
    fontSize: 14,
    lineHeight: 19,
    fontFamily: Typography.uiFontMedium,
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
