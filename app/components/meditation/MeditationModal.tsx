import React, { useEffect, useMemo } from "react";
import {
  ImageBackground,
  Modal,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import { setAudioModeAsync, useAudioPlayer, useAudioPlayerStatus } from "expo-audio";
import { LinearGradient } from "expo-linear-gradient";
import Animated, {
  Easing,
  interpolate,
  useAnimatedStyle,
  useSharedValue,
  withRepeat,
  withTiming,
} from "react-native-reanimated";
import { SafeAreaView } from "react-native-safe-area-context";
import { Colors, Radii, Typography, shadow } from "../../constants/tokens";
import { AnimatedPressable } from "../ui/AnimatedPressable";

const MEDITATION_BACKGROUND = require("../../assets/theme/meditation-sky-garden.webp");
const MEDITATION_AUDIO = require("../../assets/audio/meditation-ambient.m4a");

type MeditationModalProps = {
  visible: boolean;
  colors: typeof Colors;
  onClose: () => void;
};

export function MeditationModal({
  visible,
  colors,
  onClose,
}: MeditationModalProps) {
  const styles = useMemo(() => createStyles(colors), [colors]);
  const player = useAudioPlayer(MEDITATION_AUDIO);
  const status = useAudioPlayerStatus(player);
  const breath = useSharedValue(0);

  useEffect(() => {
    setAudioModeAsync({
      playsInSilentMode: true,
      shouldPlayInBackground: false,
      interruptionMode: "duckOthers",
    }).catch(() => {});
  }, []);

  useEffect(() => {
    player.loop = true;
    player.volume = 0.42;
    if (visible) {
      breath.value = withRepeat(
        withTiming(1, {
          duration: 5200,
          easing: Easing.inOut(Easing.sin),
        }),
        -1,
        true,
      );
      player.play();
    } else {
      player.pause();
      breath.value = withTiming(0, { duration: 420 });
    }
  }, [breath, player, visible]);

  const orbStyle = useAnimatedStyle(() => {
    const scale = interpolate(breath.value, [0, 1], [0.86, 1.16]);
    const opacity = interpolate(breath.value, [0, 1], [0.72, 0.96]);
    return {
      opacity,
      transform: [{ scale }],
    };
  });

  const haloStyle = useAnimatedStyle(() => {
    const scale = interpolate(breath.value, [0, 1], [1.05, 1.48]);
    const opacity = interpolate(breath.value, [0, 1], [0.18, 0.04]);
    return {
      opacity,
      transform: [{ scale }],
    };
  });

  const toggleAudio = () => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    if (status.playing) {
      player.pause();
    } else {
      player.play();
    }
  };

  const close = () => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    onClose();
  };

  return (
    <Modal visible={visible} animationType="fade" presentationStyle="fullScreen">
      <ImageBackground
        source={MEDITATION_BACKGROUND}
        resizeMode="cover"
        style={styles.root}
      >
        <LinearGradient
          colors={[
            "rgba(5,10,22,0.20)",
            "rgba(5,10,22,0.16)",
            "rgba(5,10,22,0.62)",
          ]}
          locations={[0, 0.52, 1]}
          style={StyleSheet.absoluteFill}
        />
        <SafeAreaView style={styles.safe} edges={["top", "bottom"]}>
          <View style={styles.topBar}>
            <Text style={styles.kicker}>Quiet Mode</Text>
            <AnimatedPressable
              style={styles.closeButton}
              preset="press"
              scale={0.94}
              onPress={close}
              accessibilityLabel="Close meditation"
            >
              <Ionicons name="close" size={20} color="#F7FAFF" />
            </AnimatedPressable>
          </View>

          <View style={styles.center}>
            <View style={styles.orbWrap}>
              <Animated.View style={[styles.orbHalo, haloStyle]} />
              <Animated.View style={[styles.orb, orbStyle]}>
                <LinearGradient
                  colors={[
                    "rgba(255,255,255,0.88)",
                    "rgba(108,174,255,0.36)",
                    "rgba(42,99,202,0.22)",
                  ]}
                  start={{ x: 0.24, y: 0.08 }}
                  end={{ x: 0.78, y: 0.92 }}
                  style={StyleSheet.absoluteFill}
                />
              </Animated.View>
            </View>
            <Text style={styles.title}>Breathe</Text>
            <Text style={styles.subtitle}>Let the build finish somewhere else for a minute.</Text>
          </View>

          <View style={styles.bottom}>
            <View style={styles.breathRow}>
              <View style={styles.breathTick} />
              <View style={[styles.breathTick, styles.breathTickSoft]} />
              <View style={styles.breathTick} />
            </View>
            <AnimatedPressable
              style={styles.audioButton}
              preset="press"
              scale={0.95}
              onPress={toggleAudio}
              accessibilityLabel={status.playing ? "Pause music" : "Play music"}
            >
              <Ionicons
                name={status.playing ? "pause" : "play"}
                size={18}
                color="#07111F"
              />
              <Text style={styles.audioButtonText}>
                {status.playing ? "Ambient playing" : "Play ambient"}
              </Text>
            </AnimatedPressable>
          </View>
        </SafeAreaView>
      </ImageBackground>
    </Modal>
  );
}

function createStyles(_colors: typeof Colors) {
  return StyleSheet.create({
    root: {
      flex: 1,
      backgroundColor: "#07111F",
    },
    safe: {
      flex: 1,
      paddingHorizontal: 22,
    },
    topBar: {
      minHeight: 56,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
    },
    kicker: {
      color: "rgba(247,250,255,0.76)",
      fontSize: 12,
      fontFamily: Typography.uiFontMedium,
      letterSpacing: 1.2,
      textTransform: "uppercase",
    },
    closeButton: {
      width: 42,
      height: 42,
      borderRadius: 21,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: "rgba(255,255,255,0.12)",
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: "rgba(255,255,255,0.22)",
    },
    center: {
      flex: 1,
      alignItems: "center",
      justifyContent: "center",
      paddingBottom: 30,
    },
    orbWrap: {
      width: 196,
      height: 196,
      alignItems: "center",
      justifyContent: "center",
      marginBottom: 34,
    },
    orbHalo: {
      position: "absolute",
      width: 178,
      height: 178,
      borderRadius: 89,
      backgroundColor: "rgba(128,190,255,0.72)",
    },
    orb: {
      width: 134,
      height: 134,
      borderRadius: 67,
      overflow: "hidden",
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: "rgba(255,255,255,0.54)",
      ...shadow("float", "#001B3D"),
    },
    title: {
      color: "#F7FAFF",
      fontSize: 34,
      lineHeight: 40,
      fontFamily: Typography.uiFontMedium,
      letterSpacing: 0,
      textAlign: "center",
    },
    subtitle: {
      maxWidth: 280,
      marginTop: 10,
      color: "rgba(247,250,255,0.74)",
      fontSize: 14,
      lineHeight: 20,
      fontFamily: Typography.uiFont,
      textAlign: "center",
    },
    bottom: {
      alignItems: "center",
      paddingBottom: 18,
      gap: 20,
    },
    breathRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 10,
      opacity: 0.72,
    },
    breathTick: {
      width: 46,
      height: 2,
      borderRadius: 1,
      backgroundColor: "rgba(255,255,255,0.74)",
    },
    breathTickSoft: {
      width: 86,
      backgroundColor: "rgba(255,255,255,0.34)",
    },
    audioButton: {
      minHeight: 50,
      paddingHorizontal: 18,
      borderRadius: Radii.pill,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "center",
      gap: 9,
      backgroundColor: "rgba(247,250,255,0.88)",
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: "rgba(255,255,255,0.76)",
    },
    audioButtonText: {
      color: "#07111F",
      fontSize: 13,
      lineHeight: 18,
      fontFamily: Typography.uiFontMedium,
    },
  });
}
