import React, { useEffect, useMemo, useState } from "react";
import {
  ImageBackground,
  Modal,
  NativeScrollEvent,
  NativeSyntheticEvent,
  ScrollView,
  StyleSheet,
  Text,
  useWindowDimensions,
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
  withSequence,
  withRepeat,
  withSpring,
  withTiming,
} from "react-native-reanimated";
import { SafeAreaView } from "react-native-safe-area-context";
import { Colors, Radii, Typography, shadow } from "../../constants/tokens";
import { AnimatedPressable } from "../ui/AnimatedPressable";

const BREATHE_BACKGROUND = require("../../assets/theme/meditation-sky-garden.webp");
const FOCUS_BACKGROUND = require("../../assets/theme/meditation-focus-dawn.webp");
const UNWIND_BACKGROUND = require("../../assets/theme/meditation-unwind-aurora.webp");
const MOKUGYO_BACKGROUND = require("../../assets/theme/meditation-mokugyo-room.webp");

const BREATHE_AUDIO = require("../../assets/audio/meditation-ambient.m4a");
const FOCUS_AUDIO = require("../../assets/audio/meditation-focus.m4a");
const UNWIND_AUDIO = require("../../assets/audio/meditation-unwind.m4a");
const MOKUGYO_HIT = require("../../assets/audio/mokugyo-hit.m4a");

const MEDITATION_MODES = [
  {
    key: "breathe",
    kind: "ambient",
    title: "Breathe",
    subtitle: "Let the build finish somewhere else for a minute.",
    cue: "Slow air",
    background: BREATHE_BACKGROUND,
    audio: "breathe",
    duration: 5200,
    volume: 0.42,
    orb: ["rgba(255,255,255,0.88)", "rgba(108,174,255,0.36)", "rgba(42,99,202,0.22)"] as const,
    halo: "rgba(128,190,255,0.72)",
    overlay: ["rgba(5,10,22,0.20)", "rgba(5,10,22,0.16)", "rgba(5,10,22,0.62)"] as const,
  },
  {
    key: "focus",
    kind: "ambient",
    title: "Focus",
    subtitle: "A clear blue pause before the next decision.",
    cue: "Dawn lake",
    background: FOCUS_BACKGROUND,
    audio: "focus",
    duration: 4200,
    volume: 0.34,
    orb: ["rgba(222,246,255,0.90)", "rgba(64,190,255,0.34)", "rgba(26,82,170,0.24)"] as const,
    halo: "rgba(64,190,255,0.64)",
    overlay: ["rgba(2,16,42,0.12)", "rgba(8,39,82,0.10)", "rgba(2,15,34,0.58)"] as const,
  },
  {
    key: "unwind",
    kind: "ambient",
    title: "Unwind",
    subtitle: "Drop the stack trace from your shoulders.",
    cue: "Aurora field",
    background: UNWIND_BACKGROUND,
    audio: "unwind",
    duration: 7200,
    volume: 0.46,
    orb: ["rgba(255,255,245,0.88)", "rgba(172,146,255,0.30)", "rgba(58,46,144,0.24)"] as const,
    halo: "rgba(172,146,255,0.58)",
    overlay: ["rgba(0,6,28,0.06)", "rgba(15,9,54,0.08)", "rgba(5,5,22,0.64)"] as const,
  },
  {
    key: "mokugyo",
    kind: "mokugyo",
    title: "Mokugyo",
    subtitle: "Tap once. Let the sound disappear before the next strike.",
    cue: "Wooden fish",
    background: MOKUGYO_BACKGROUND,
    duration: 3600,
    volume: 0,
    orb: ["rgba(255,244,220,0.92)", "rgba(171,99,38,0.44)", "rgba(66,35,16,0.26)"] as const,
    halo: "rgba(246,185,92,0.58)",
    overlay: ["rgba(10,6,2,0.08)", "rgba(25,14,8,0.18)", "rgba(4,4,8,0.70)"] as const,
  },
] as const;

type AmbientMode = Extract<(typeof MEDITATION_MODES)[number], { kind: "ambient" }>;
type MeditationMode = (typeof MEDITATION_MODES)[number];

type MeditationModalProps = {
  visible: boolean;
  colors: typeof Colors;
  onClose: () => void;
};

function isAmbientMode(mode: MeditationMode): mode is AmbientMode {
  return mode.kind === "ambient";
}

export function MeditationModal({
  visible,
  colors,
  onClose,
}: MeditationModalProps) {
  const styles = useMemo(() => createStyles(colors), [colors]);
  const { width } = useWindowDimensions();
  const [modeIndex, setModeIndex] = useState(0);
  const [mokugyoCount, setMokugyoCount] = useState(0);
  const mode = MEDITATION_MODES[modeIndex] ?? MEDITATION_MODES[0];
  const breathePlayer = useAudioPlayer(BREATHE_AUDIO);
  const focusPlayer = useAudioPlayer(FOCUS_AUDIO);
  const unwindPlayer = useAudioPlayer(UNWIND_AUDIO);
  const mokugyoPlayer = useAudioPlayer(MOKUGYO_HIT);
  const breatheStatus = useAudioPlayerStatus(breathePlayer);
  const focusStatus = useAudioPlayerStatus(focusPlayer);
  const unwindStatus = useAudioPlayerStatus(unwindPlayer);
  const breath = useSharedValue(0);
  const strike = useSharedValue(0);
  const ripple = useSharedValue(0);

  const ambientPlayers = useMemo(
    () => ({
      breathe: breathePlayer,
      focus: focusPlayer,
      unwind: unwindPlayer,
    }),
    [breathePlayer, focusPlayer, unwindPlayer],
  );
  const ambientStatuses = {
    breathe: breatheStatus,
    focus: focusStatus,
    unwind: unwindStatus,
  };
  const activeAmbientPlayer = isAmbientMode(mode) ? ambientPlayers[mode.audio] : null;
  const activeAmbientStatus = isAmbientMode(mode) ? ambientStatuses[mode.audio] : null;

  useEffect(() => {
    setAudioModeAsync({
      playsInSilentMode: true,
      shouldPlayInBackground: false,
      interruptionMode: "duckOthers",
    }).catch(() => {});
  }, []);

  useEffect(() => {
    breathePlayer.loop = true;
    focusPlayer.loop = true;
    unwindPlayer.loop = true;
    mokugyoPlayer.loop = false;
  }, [breathePlayer, focusPlayer, mokugyoPlayer, unwindPlayer]);

  useEffect(() => {
    breathePlayer.pause();
    focusPlayer.pause();
    unwindPlayer.pause();

    if (visible && isAmbientMode(mode)) {
      const nextPlayer = ambientPlayers[mode.audio];
      nextPlayer.volume = mode.volume;
      breath.value = withRepeat(
        withTiming(1, {
          duration: mode.duration,
          easing: Easing.inOut(Easing.sin),
        }),
        -1,
        true,
      );
      nextPlayer.play();
    } else {
      breath.value = withTiming(0, { duration: 420 });
    }
  }, [
    ambientPlayers,
    breath,
    breathePlayer,
    focusPlayer,
    mode,
    unwindPlayer,
    visible,
  ]);

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

  const fishStyle = useAnimatedStyle(() => {
    const scale = interpolate(strike.value, [0, 1], [1, 0.94]);
    const translateY = interpolate(strike.value, [0, 1], [0, 8]);
    const rotate = interpolate(strike.value, [0, 1], [0, -1.5]);
    return {
      transform: [{ translateY }, { rotate: `${rotate}deg` }, { scale }],
    };
  });

  const malletStyle = useAnimatedStyle(() => {
    const translateY = interpolate(strike.value, [0, 1], [-28, 18]);
    const translateX = interpolate(strike.value, [0, 1], [18, -8]);
    const rotate = interpolate(strike.value, [0, 1], [-20, -38]);
    return {
      transform: [{ translateX }, { translateY }, { rotate: `${rotate}deg` }],
    };
  });

  const ringStyle = useAnimatedStyle(() => {
    const scale = interpolate(ripple.value, [0, 1], [0.78, 1.5]);
    const opacity = interpolate(ripple.value, [0, 1], [0.30, 0]);
    return {
      opacity,
      transform: [{ scale }],
    };
  });

  const toggleAudio = () => {
    if (!activeAmbientPlayer || !activeAmbientStatus) {
      strikeMokugyo();
      return;
    }
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    if (activeAmbientStatus.playing) {
      activeAmbientPlayer.pause();
    } else {
      activeAmbientPlayer.play();
    }
  };

  const strikeMokugyo = () => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    mokugyoPlayer.volume = 0.92;
    mokugyoPlayer.seekTo(0).finally(() => {
      mokugyoPlayer.play();
    });
    setMokugyoCount((value) => value + 1);
    strike.value = withSequence(
      withTiming(1, { duration: 68, easing: Easing.out(Easing.quad) }),
      withSpring(0, { damping: 10, stiffness: 170, mass: 0.7 }),
    );
    ripple.value = 0;
    ripple.value = withTiming(1, { duration: 620, easing: Easing.out(Easing.cubic) });
  };

  const close = () => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    breathePlayer.pause();
    focusPlayer.pause();
    unwindPlayer.pause();
    onClose();
  };

  const handleModeScrollEnd = (event: NativeSyntheticEvent<NativeScrollEvent>) => {
    const nextIndex = Math.round(event.nativeEvent.contentOffset.x / Math.max(width, 1));
    const clamped = Math.max(0, Math.min(MEDITATION_MODES.length - 1, nextIndex));
    if (clamped !== modeIndex) {
      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
      setModeIndex(clamped);
    }
  };

  return (
    <Modal visible={visible} animationType="fade" presentationStyle="fullScreen">
      <ImageBackground
        source={mode.background}
        resizeMode="cover"
        style={styles.root}
      >
        <LinearGradient
          colors={mode.overlay}
          locations={[0, 0.52, 1]}
          style={StyleSheet.absoluteFill}
        />
        <SafeAreaView style={styles.safe} edges={["top", "bottom"]}>
          <View style={styles.topBar}>
            <Text style={styles.kicker}>Quiet Mode · {mode.cue}</Text>
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

          <ScrollView
            horizontal
            pagingEnabled
            bounces={false}
            showsHorizontalScrollIndicator={false}
            onMomentumScrollEnd={handleModeScrollEnd}
            style={styles.modePager}
          >
            {MEDITATION_MODES.map((item) => (
              <View key={item.key} style={[styles.center, { width }]}>
                {item.kind === "mokugyo" ? (
                  <AnimatedPressable
                    style={styles.mokugyoStage}
                    preset="press"
                    scale={0.98}
                    onPress={strikeMokugyo}
                    accessibilityLabel="Strike wooden fish"
                  >
                    <Animated.View style={[styles.mokugyoRing, ringStyle]} />
                    <Animated.View style={[styles.mallet, malletStyle]}>
                      <View style={styles.malletHead} />
                      <View style={styles.malletHandle} />
                    </Animated.View>
                    <Animated.View style={[styles.fish, fishStyle]}>
                      <LinearGradient
                        colors={["#E3A35A", "#9B571F", "#4D260E"]}
                        start={{ x: 0.18, y: 0.08 }}
                        end={{ x: 0.82, y: 0.92 }}
                        style={StyleSheet.absoluteFill}
                      />
                      <View style={styles.fishHighlight} />
                      <View style={styles.fishMouth} />
                      <View style={styles.fishGroove} />
                    </Animated.View>
                    <Text style={styles.mokugyoCounter}>
                      {mokugyoCount === 0 ? "Tap" : `${mokugyoCount}`}
                    </Text>
                  </AnimatedPressable>
                ) : (
                  <View style={styles.orbWrap}>
                    <Animated.View
                      style={[
                        styles.orbHalo,
                        { backgroundColor: item.halo },
                        haloStyle,
                      ]}
                    />
                    <Animated.View style={[styles.orb, orbStyle]}>
                      <LinearGradient
                        colors={item.orb}
                        start={{ x: 0.24, y: 0.08 }}
                        end={{ x: 0.78, y: 0.92 }}
                        style={StyleSheet.absoluteFill}
                      />
                    </Animated.View>
                  </View>
                )}
                <Text style={styles.title}>{item.title}</Text>
                <Text style={styles.subtitle}>{item.subtitle}</Text>
              </View>
            ))}
          </ScrollView>

          <View style={styles.bottom}>
            <View style={styles.modeDots}>
              {MEDITATION_MODES.map((item, index) => (
                <View
                  key={item.key}
                  style={[
                    styles.modeDot,
                    index === modeIndex && styles.modeDotActive,
                  ]}
                />
              ))}
            </View>
            <View style={styles.breathRow}>
              {mode.key === "mokugyo" ? (
                <>
                  <View style={[styles.breathTick, styles.woodTick]} />
                  <View style={[styles.breathTick, styles.woodTickSoft]} />
                  <View style={[styles.breathTick, styles.woodTick]} />
                </>
              ) : (
                <>
                  <View style={styles.breathTick} />
                  <View style={[styles.breathTick, styles.breathTickSoft]} />
                  <View style={styles.breathTick} />
                </>
              )}
            </View>
            <AnimatedPressable
              style={[
                styles.audioButton,
                mode.key === "mokugyo" && styles.mokugyoButton,
              ]}
              preset="press"
              scale={0.95}
              onPress={toggleAudio}
              accessibilityLabel={
                mode.key === "mokugyo"
                  ? "Strike wooden fish"
                  : activeAmbientStatus?.playing
                    ? "Pause music"
                    : "Play music"
              }
            >
              <Ionicons
                name={
                  mode.key === "mokugyo"
                    ? "radio-button-on"
                    : activeAmbientStatus?.playing
                      ? "pause"
                      : "play"
                }
                size={18}
                color="#07111F"
              />
              <Text style={styles.audioButtonText}>
                {mode.key === "mokugyo"
                  ? "Strike wooden fish"
                  : activeAmbientStatus?.playing
                    ? `${mode.title} playing`
                    : `Play ${mode.title.toLowerCase()}`}
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
    modePager: {
      flex: 1,
      marginHorizontal: -22,
    },
    center: {
      flex: 1,
      alignItems: "center",
      justifyContent: "center",
      paddingHorizontal: 22,
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
    mokugyoStage: {
      width: 232,
      height: 216,
      alignItems: "center",
      justifyContent: "center",
      marginBottom: 30,
    },
    mokugyoRing: {
      position: "absolute",
      width: 178,
      height: 178,
      borderRadius: 89,
      borderWidth: 2,
      borderColor: "rgba(255,226,166,0.58)",
      backgroundColor: "rgba(255,207,117,0.08)",
    },
    mallet: {
      position: "absolute",
      top: 8,
      right: 18,
      width: 92,
      height: 116,
      alignItems: "center",
      zIndex: 2,
    },
    malletHead: {
      width: 64,
      height: 24,
      borderRadius: 12,
      backgroundColor: "#B97634",
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: "rgba(255,230,185,0.58)",
      ...shadow("raised", "#2D1406"),
    },
    malletHandle: {
      width: 10,
      height: 86,
      marginTop: -2,
      borderRadius: 5,
      backgroundColor: "#7A4118",
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: "rgba(255,218,164,0.34)",
    },
    fish: {
      width: 180,
      height: 126,
      borderTopLeftRadius: 86,
      borderTopRightRadius: 78,
      borderBottomLeftRadius: 64,
      borderBottomRightRadius: 72,
      overflow: "hidden",
      borderWidth: 1,
      borderColor: "rgba(255,224,170,0.56)",
      ...shadow("float", "#2D1406"),
    },
    fishHighlight: {
      position: "absolute",
      top: 18,
      left: 34,
      width: 86,
      height: 20,
      borderRadius: 10,
      backgroundColor: "rgba(255,231,181,0.26)",
      transform: [{ rotate: "-9deg" }],
    },
    fishMouth: {
      position: "absolute",
      top: 46,
      right: 28,
      width: 42,
      height: 18,
      borderRadius: 10,
      backgroundColor: "rgba(34,14,5,0.76)",
      transform: [{ rotate: "-8deg" }],
    },
    fishGroove: {
      position: "absolute",
      left: 42,
      right: 34,
      bottom: 30,
      height: 10,
      borderRadius: 5,
      backgroundColor: "rgba(49,20,7,0.50)",
      transform: [{ rotate: "3deg" }],
    },
    mokugyoCounter: {
      position: "absolute",
      bottom: 0,
      minWidth: 58,
      paddingHorizontal: 14,
      paddingVertical: 7,
      borderRadius: Radii.pill,
      overflow: "hidden",
      color: "#2D1406",
      fontSize: 13,
      lineHeight: 18,
      fontFamily: Typography.uiFontMedium,
      textAlign: "center",
      backgroundColor: "rgba(255,232,190,0.86)",
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: "rgba(255,244,220,0.76)",
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
    modeDots: {
      flexDirection: "row",
      alignItems: "center",
      gap: 7,
    },
    modeDot: {
      width: 6,
      height: 6,
      borderRadius: 3,
      backgroundColor: "rgba(255,255,255,0.28)",
    },
    modeDotActive: {
      width: 18,
      backgroundColor: "rgba(255,255,255,0.84)",
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
    woodTick: {
      backgroundColor: "rgba(255,220,165,0.78)",
    },
    woodTickSoft: {
      width: 86,
      backgroundColor: "rgba(255,186,105,0.42)",
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
    mokugyoButton: {
      backgroundColor: "rgba(255,225,178,0.90)",
      borderColor: "rgba(255,239,211,0.82)",
    },
    audioButtonText: {
      color: "#07111F",
      fontSize: 13,
      lineHeight: 18,
      fontFamily: Typography.uiFontMedium,
    },
  });
}
