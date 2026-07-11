import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Modal,
  NativeScrollEvent,
  NativeSyntheticEvent,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  useWindowDimensions,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import { setAudioModeAsync, useAudioPlayer } from "expo-audio";
import { LinearGradient } from "expo-linear-gradient";
import { useVideoPlayer, VideoView } from "expo-video";
import Animated, {
  Easing,
  interpolate,
  useAnimatedStyle,
  useSharedValue,
  withDelay,
  withRepeat,
  withSequence,
  withSpring,
  withTiming,
} from "react-native-reanimated";
import { SafeAreaView } from "react-native-safe-area-context";
import Svg, {
  Defs,
  Ellipse,
  G,
  LinearGradient as SvgLinearGradient,
  Path,
  RadialGradient,
  Stop,
} from "react-native-svg";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import {
  QUIET_MODES,
  resolveWindowClipEndMs,
} from "./quietModes";
import { fetchWindowScenePage, type WindowScene } from "./windowScenes";

const MEDITATION_AUDIO = require("../../assets/audio/meditation-ambient.m4a");
const MOKUGYO_HIT = require("../../assets/audio/mokugyo-hit-jono.m4a");

const MEDITATION_BREATH_MS = 6400;
const MEDITATION_VOLUME = 0.42;
const WINDOW_FADE_MS = 180;
const CHROME_AUTO_HIDE_MS = 2800;
const CREDIT_AUTO_HIDE_MS = 3200;

type MeditationModalProps = {
  visible: boolean;
  colors: unknown;
  onClose: () => void;
};

export function MeditationModal({ visible, onClose }: MeditationModalProps) {
  const styles = useMemo(() => createStyles(), []);
  const { width, height } = useWindowDimensions();
  const pagerRef = useRef<ScrollView>(null);
  const switchingWindowRef = useRef(false);
  const windowQueueRef = useRef<WindowScene[]>([]);
  const windowCursorRef = useRef<string | null>(null);
  const seenWindowIdsRef = useRef(new Set<string>());
  const clipEndTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const chromeHideTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const creditHideTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [modeIndex, setModeIndex] = useState(0);
  const [windowMuted, setWindowMuted] = useState(false);
  const [windowScene, setWindowScene] = useState<WindowScene | null>(null);
  const [windowLoading, setWindowLoading] = useState(false);
  const [windowNetworkError, setWindowNetworkError] = useState(false);
  const [chromeVisible, setChromeVisible] = useState(false);
  const [creditVisible, setCreditVisible] = useState(false);
  const mode = QUIET_MODES[modeIndex] ?? QUIET_MODES[0];

  const meditationPlayer = useAudioPlayer(MEDITATION_AUDIO);
  const mokugyoPlayer = useAudioPlayer(MOKUGYO_HIT);

  const windowPlayer = useVideoPlayer(null, (player) => {
    player.loop = false;
    player.muted = false;
  });
  const breath = useSharedValue(0);
  const strike = useSharedValue(0);
  const strikePop = useSharedValue(0);
  const windowOpacity = useSharedValue(1);

  const clearClipEndTimer = useCallback(() => {
    if (clipEndTimerRef.current) {
      clearTimeout(clipEndTimerRef.current);
      clipEndTimerRef.current = null;
    }
  }, []);

  const clearChromeTimer = useCallback(() => {
    if (chromeHideTimerRef.current) {
      clearTimeout(chromeHideTimerRef.current);
      chromeHideTimerRef.current = null;
    }
  }, []);

  const clearCreditTimer = useCallback(() => {
    if (creditHideTimerRef.current) {
      clearTimeout(creditHideTimerRef.current);
      creditHideTimerRef.current = null;
    }
  }, []);

  const scheduleChromeHide = useCallback(() => {
    clearChromeTimer();
    chromeHideTimerRef.current = setTimeout(() => {
      setChromeVisible(false);
      chromeHideTimerRef.current = null;
    }, CHROME_AUTO_HIDE_MS);
  }, [clearChromeTimer]);

  const showCreditBriefly = useCallback(() => {
    clearCreditTimer();
    setCreditVisible(true);
    creditHideTimerRef.current = setTimeout(() => {
      setCreditVisible(false);
      creditHideTimerRef.current = null;
    }, CREDIT_AUTO_HIDE_MS);
  }, [clearCreditTimer]);

  const revealChrome = useCallback(() => {
    setChromeVisible(true);
    scheduleChromeHide();
  }, [scheduleChromeHide]);

  const pauseAllMedia = useCallback(() => {
    meditationPlayer.pause();
    mokugyoPlayer.pause();
    windowPlayer.pause();
  }, [meditationPlayer, mokugyoPlayer, windowPlayer]);

  const scheduleClipEnd = useCallback(
    (onEnd: () => void) => {
      clearClipEndTimer();
      const arm = () => {
        clearClipEndTimer();
        const naturalMs =
          windowPlayer.duration > 0
            ? Math.round(windowPlayer.duration * 1000)
            : null;
        const waitMs = Math.max(400, resolveWindowClipEndMs(naturalMs));
        clipEndTimerRef.current = setTimeout(() => {
          clipEndTimerRef.current = null;
          onEnd();
        }, waitMs);
      };
      if (windowPlayer.duration > 0) {
        arm();
        return;
      }
      const sub = windowPlayer.addListener("sourceLoad", () => {
        sub.remove();
        arm();
      });
      // Fallback if sourceLoad already fired or never fires.
      clipEndTimerRef.current = setTimeout(arm, 800);
    },
    [clearClipEndTimer, windowPlayer],
  );

  const switchWindow = useCallback(
    async (scene: WindowScene) => {
      if (switchingWindowRef.current) {
        return;
      }
      switchingWindowRef.current = true;
      clearClipEndTimer();
      try {
        setWindowLoading(true);
        setWindowNetworkError(false);
        windowOpacity.value = withTiming(0, { duration: WINDOW_FADE_MS });
        await new Promise((resolve) => setTimeout(resolve, WINDOW_FADE_MS));
        windowPlayer.pause();
        await windowPlayer.replaceAsync({ uri: scene.uri });
        windowPlayer.loop = false;
        windowPlayer.muted = windowMuted;
        windowPlayer.currentTime = 0;
        seenWindowIdsRef.current.add(scene.id);
        setWindowScene(scene);
        showCreditBriefly();
        windowPlayer.play();
        windowOpacity.value = withTiming(1, {
          duration: WINDOW_FADE_MS,
          easing: Easing.out(Easing.quad),
        });
        switchingWindowRef.current = false;
        setWindowLoading(false);
        scheduleClipEnd(() => void advanceWindowRef.current(false));
      } catch {
        windowOpacity.value = withTiming(1, { duration: WINDOW_FADE_MS });
        switchingWindowRef.current = false;
        setWindowLoading(false);
        void advanceWindowRef.current(false);
      }
    },
    [
      clearClipEndTimer,
      scheduleClipEnd,
      showCreditBriefly,
      windowOpacity,
      windowPlayer,
      windowMuted,
    ],
  );

  const advanceWindow = useCallback(
    async (manual: boolean) => {
      if (mode.key !== "window" || switchingWindowRef.current) {
        return;
      }
      if (manual) {
        Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
        scheduleChromeHide();
      }
      if (windowQueueRef.current.length < 4) {
        try {
          const page = await fetchWindowScenePage(windowCursorRef.current);
          setWindowNetworkError(false);
          windowCursorRef.current = page.cursor;
          const unseen = page.scenes.filter(
            (scene) => !seenWindowIdsRef.current.has(scene.id),
          );
          windowQueueRef.current.push(...unseen);
        } catch {
          setWindowNetworkError(true);
          switchingWindowRef.current = false;
        }
      }
      const next = windowQueueRef.current.shift();
      if (next) {
        void switchWindow(next);
      }
    },
    [mode.key, scheduleChromeHide, switchWindow],
  );

  const advanceWindowRef = useRef(advanceWindow);
  advanceWindowRef.current = advanceWindow;

  useEffect(() => {
    setAudioModeAsync({
      playsInSilentMode: true,
      shouldPlayInBackground: false,
      interruptionMode: "duckOthers",
    }).catch(() => {});
  }, []);

  useEffect(() => {
    meditationPlayer.loop = true;
    mokugyoPlayer.loop = false;
  }, [meditationPlayer, mokugyoPlayer]);

  useEffect(() => {
    windowPlayer.muted = windowMuted;
  }, [windowPlayer, windowMuted]);

  useEffect(() => {
    if (!visible) {
      return;
    }
    const controller = new AbortController();
    windowQueueRef.current = [];
    windowCursorRef.current = null;
    seenWindowIdsRef.current.clear();
    setWindowScene(null);
    setWindowLoading(true);
    setWindowNetworkError(false);
    windowOpacity.value = 1;
    void fetchWindowScenePage(null, controller.signal)
      .then((page) => {
        windowCursorRef.current = page.cursor;
        windowQueueRef.current = page.scenes;
        setWindowLoading(false);
        setWindowNetworkError(false);
      })
      .catch(() => {
        setWindowLoading(false);
        setWindowNetworkError(true);
      });
    return () => controller.abort();
  }, [visible, windowOpacity]);

  useEffect(() => {
    meditationPlayer.pause();
    mokugyoPlayer.pause();
    windowPlayer.pause();
    clearChromeTimer();
    clearCreditTimer();
    clearClipEndTimer();
    setChromeVisible(false);

    if (!visible) {
      breath.value = withTiming(0, { duration: 420 });
      return;
    }

    if (mode.key === "meditation") {
      meditationPlayer.volume = MEDITATION_VOLUME;
      breath.value = withRepeat(
        withTiming(1, {
          duration: MEDITATION_BREATH_MS,
          easing: Easing.inOut(Easing.sin),
        }),
        -1,
        true,
      );
      meditationPlayer.play();
    } else {
      breath.value = withTiming(0, { duration: 420 });
    }

    if (mode.key === "window") {
      if (windowScene) {
        windowPlayer.loop = false;
        windowPlayer.muted = windowMuted;
        windowPlayer.play();
        windowOpacity.value = 1;
        showCreditBriefly();
        scheduleClipEnd(() => void advanceWindowRef.current(false));
      } else if (!windowLoading) {
        void advanceWindowRef.current(false);
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- mode/visibility media exclusivity only
  }, [
    breath,
    clearChromeTimer,
    clearClipEndTimer,
    clearCreditTimer,
    meditationPlayer,
    mode.key,
    mokugyoPlayer,
    scheduleClipEnd,
    showCreditBriefly,
    visible,
    windowOpacity,
    windowPlayer,
    windowLoading,
    windowScene,
  ]);

  useEffect(() => {
    if (!visible || mode.key !== "window") {
      return;
    }
    const endSub = windowPlayer.addListener("playToEnd", () => {
      clearClipEndTimer();
      void advanceWindowRef.current(false);
    });
    const statusSub = windowPlayer.addListener("statusChange", ({ status }) => {
      if (status === "error" && !switchingWindowRef.current) {
        clearClipEndTimer();
        void advanceWindowRef.current(false);
      }
    });
    return () => {
      endSub.remove();
      statusSub.remove();
    };
  }, [
    clearClipEndTimer,
    mode.key,
    visible,
    windowPlayer,
  ]);

  useEffect(() => {
    return () => {
      clearChromeTimer();
      clearCreditTimer();
      clearClipEndTimer();
    };
  }, [clearChromeTimer, clearClipEndTimer, clearCreditTimer]);

  const fieldCoreStyle = useAnimatedStyle(() => {
    const scale = interpolate(breath.value, [0, 1], [0.92, 1.08]);
    const opacity = interpolate(breath.value, [0, 1], [0.5, 0.82]);
    return {
      opacity,
      transform: [{ scale }],
    };
  });

  const fieldGlowStyle = useAnimatedStyle(() => {
    const scale = interpolate(breath.value, [0, 1], [0.86, 1.24]);
    const opacity = interpolate(breath.value, [0, 1], [0.12, 0.32]);
    return {
      opacity,
      transform: [{ scale }],
    };
  });

  // Cloth head pivots onto the ridge beside the slit; shaft stays clear.
  const malletStyle = useAnimatedStyle(() => {
    const rotate = interpolate(strike.value, [0, 1], [-34, 4]);
    const translateX = interpolate(strike.value, [0, 1], [42, -2]);
    const translateY = interpolate(strike.value, [0, 1], [-88, 8]);
    return {
      transform: [{ translateX }, { translateY }, { rotate: `${rotate}deg` }],
    };
  });

  const strikePopStyle = useAnimatedStyle(() => ({
    opacity: interpolate(strikePop.value, [0, 0.18, 0.82, 1], [0, 1, 1, 0]),
    transform: [
      { translateY: interpolate(strikePop.value, [0, 1], [4, -42]) },
      { scale: interpolate(strikePop.value, [0, 0.2, 1], [0.72, 1, 1.04]) },
    ],
  }));

  const windowStyle = useAnimatedStyle(() => ({
    opacity: windowOpacity.value,
  }));

  const strikeMokugyo = () => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    mokugyoPlayer.volume = 0.94;
    mokugyoPlayer.seekTo(0).finally(() => {
      mokugyoPlayer.play();
    });
    strike.value = withSequence(
      withTiming(1, { duration: 52, easing: Easing.out(Easing.quad) }),
      withSpring(0, { damping: 14, stiffness: 240, mass: 0.45 }),
    );
    strikePop.value = 0;
    strikePop.value = withSequence(
      withTiming(0.2, { duration: 70 }),
      withDelay(260, withTiming(1, { duration: 420, easing: Easing.out(Easing.quad) })),
    );
  };

  const selectMode = (index: number) => {
    const clamped = Math.max(0, Math.min(QUIET_MODES.length - 1, index));
    if (clamped === modeIndex) {
      return;
    }
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    setModeIndex(clamped);
    pagerRef.current?.scrollTo({ x: clamped * width, animated: true });
  };

  const toggleWindowMute = () => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    setWindowMuted((value) => !value);
    scheduleChromeHide();
  };

  const close = () => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    pauseAllMedia();
    onClose();
  };

  const handleModeScrollEnd = (
    event: NativeSyntheticEvent<NativeScrollEvent>,
  ) => {
    const nextIndex = Math.round(
      event.nativeEvent.contentOffset.x / Math.max(width, 1),
    );
    const clamped = Math.max(0, Math.min(QUIET_MODES.length - 1, nextIndex));
    if (clamped !== modeIndex) {
      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
      setModeIndex(clamped);
    }
  };

  const handleStageTap = () => {
    if (mode.key === "window") {
      if (chromeVisible) {
        setChromeVisible(false);
        clearChromeTimer();
      } else {
        revealChrome();
      }
      return;
    }
    revealChrome();
  };

  const overlayColors =
    mode.key === "mokugyo"
      ? (["rgba(8,5,3,0.35)", "rgba(12,8,5,0.55)", "rgba(4,3,2,0.88)"] as const)
      : mode.key === "window"
        ? (["rgba(0,0,0,0.15)", "rgba(0,0,0,0.05)", "rgba(0,0,0,0.35)"] as const)
        : (["rgba(4,8,7,0.55)", "rgba(3,6,6,0.72)", "rgba(2,4,4,0.92)"] as const);

  return (
    <Modal
      visible={visible}
      animationType="fade"
      presentationStyle="fullScreen"
      onRequestClose={close}
    >
      <View style={styles.root}>
        <LinearGradient
          colors={overlayColors}
          locations={[0, 0.48, 1]}
          style={StyleSheet.absoluteFill}
          pointerEvents="none"
        />
        <ScrollView
          ref={pagerRef}
          horizontal
          pagingEnabled
          bounces={false}
          showsHorizontalScrollIndicator={false}
          onMomentumScrollEnd={handleModeScrollEnd}
          style={styles.modePager}
        >
          {QUIET_MODES.map((item) => (
            <View key={item.key} style={[styles.page, { width, height }]}>
              {item.key === "meditation" ? (
                <Pressable
                  style={styles.fullStage}
                  onPress={handleStageTap}
                  accessibilityLabel="Show meditation controls"
                >
                  <LinearGradient
                    colors={["#71888b", "#c8bda5", "#2c3a38"]}
                    locations={[0, 0.52, 1]}
                    style={StyleSheet.absoluteFill}
                  />
                  <View style={styles.meditationHaze} />
                  <View style={styles.mountainFar} />
                  <View style={styles.mountainNear} />
                  <View style={styles.meditationWater} />
                  <View style={styles.breathStage} accessibilityElementsHidden>
                    <Animated.View
                      style={[styles.breathGlow, fieldGlowStyle]}
                    />
                    <Animated.View
                      style={[styles.breathCore, fieldCoreStyle]}
                    />
                  </View>
                </Pressable>
              ) : null}

              {item.key === "mokugyo" ? (
                <AnimatedPressable
                  style={styles.fullStage}
                  preset="press"
                  scale={1}
                  onPress={strikeMokugyo}
                  accessibilityLabel="Strike wooden fish"
                  accessibilityRole="button"
                >
                  <View style={styles.mokugyoStage} pointerEvents="none">
                    <Animated.View style={[styles.malletWrap, malletStyle]}>
                      <MokugyoMallet />
                    </Animated.View>
                    <MokugyoInstrument />
                    <Animated.Text style={[styles.strikePop, strikePopStyle]}>
                      +1
                    </Animated.Text>
                  </View>
                </AnimatedPressable>
              ) : null}

              {item.key === "window" ? (
                <Pressable
                  style={styles.fullStage}
                  onPress={handleStageTap}
                  accessibilityLabel="Show window controls"
                >
                  <View style={styles.windowRoom}>
                    <Animated.View
                      style={[styles.windowVideo, windowStyle]}
                      pointerEvents="none"
                    >
                      <VideoView
                        style={styles.windowVideo}
                        player={windowPlayer}
                        nativeControls={false}
                        contentFit="cover"
                        pointerEvents="none"
                      />
                    </Animated.View>
                    <View style={styles.windowGlass} pointerEvents="none" />
                    {windowLoading ? (
                      <View style={styles.windowLoading} pointerEvents="none">
                        <View style={styles.windowLoadingDot} />
                      </View>
                    ) : null}
                    {windowNetworkError && !windowLoading ? (
                      <View style={styles.windowNetworkError} pointerEvents="none">
                        <Ionicons name="cloud-offline-outline" size={22} color="rgba(244,241,232,0.5)" />
                        <Text style={styles.windowNetworkErrorText}>Tap next when connected</Text>
                      </View>
                    ) : null}
                  </View>
                </Pressable>
              ) : null}
            </View>
          ))}
        </ScrollView>

        <SafeAreaView
          style={styles.chrome}
          edges={["top", "bottom"]}
          pointerEvents="box-none"
        >
          <View style={styles.topBar} pointerEvents="box-none">
            <View style={styles.modeIndicator} pointerEvents="box-none">
              {QUIET_MODES.map((item, index) => {
                const selected = index === modeIndex;
                return (
                  <Pressable
                    key={item.key}
                    onPress={() => selectMode(index)}
                    accessibilityRole="tab"
                    accessibilityState={{ selected }}
                    accessibilityLabel={`Mode ${index + 1}`}
                    hitSlop={10}
                    style={styles.modeDotHit}
                  >
                    <View
                      style={[styles.modeDot, selected && styles.modeDotActive]}
                    />
                  </Pressable>
                );
              })}
            </View>
            <AnimatedPressable
              style={styles.closeButton}
              preset="press"
              scale={0.94}
              onPress={close}
              accessibilityLabel="Close Quiet Mode"
              hitSlop={8}
            >
              <Ionicons name="close" size={18} color="rgba(244,241,232,0.72)" />
            </AnimatedPressable>
          </View>

          {mode.key === "window" && creditVisible && windowScene ? (
            <Text style={styles.creditLine} pointerEvents="none">
              {windowScene.attribution} · {windowScene.license}
            </Text>
          ) : null}

          {mode.key === "window" && chromeVisible ? (
            <View style={styles.transientControls} pointerEvents="box-none">
              <AnimatedPressable
                style={styles.ghostButton}
                preset="press"
                scale={0.95}
                onPress={() => advanceWindow(true)}
                accessibilityLabel="Next recorded window"
              >
                <Ionicons
                  name="play-skip-forward"
                  size={16}
                  color="rgba(244,241,232,0.88)"
                />
              </AnimatedPressable>
              <AnimatedPressable
                style={styles.ghostButton}
                preset="press"
                scale={0.95}
                onPress={toggleWindowMute}
                accessibilityLabel={
                  windowMuted ? "Unmute window" : "Mute window"
                }
              >
                <Ionicons
                  name={windowMuted ? "volume-mute" : "volume-high"}
                  size={16}
                  color="rgba(244,241,232,0.88)"
                />
              </AnimatedPressable>
            </View>
          ) : null}
        </SafeAreaView>
      </View>
    </Modal>
  );
}

function MokugyoInstrument() {
  return (
    <Svg width={282} height={218} viewBox="0 0 282 218">
      <Defs>
        <RadialGradient id="woodBody" cx="0.38" cy="0.2" r="0.86">
          <Stop offset="0" stopColor="#B97738" />
          <Stop offset="0.48" stopColor="#75401D" />
          <Stop offset="0.82" stopColor="#45220F" />
          <Stop offset="1" stopColor="#271006" />
        </RadialGradient>
        <SvgLinearGradient id="stand" x1="0" y1="0" x2="1" y2="1">
          <Stop offset="0" stopColor="#71302D" />
          <Stop offset="1" stopColor="#311111" />
        </SvgLinearGradient>
      </Defs>
      <Ellipse cx="141" cy="202" rx="90" ry="10" fill="rgba(0,0,0,0.42)" />
      <Ellipse cx="141" cy="184" rx="88" ry="20" fill="url(#stand)" stroke="#8E4840" strokeWidth="1.5" />
      <Ellipse cx="141" cy="180" rx="73" ry="11" fill="none" stroke="rgba(224,159,118,0.32)" strokeWidth="1.5" />
      <Path
        d="M43 120 C43 68 82 31 137 28 C186 25 228 55 239 98 C248 135 224 169 183 181 C140 194 80 181 56 153 C47 142 43 131 43 120 Z"
        fill="url(#woodBody)"
        stroke="#B87435"
        strokeWidth="2.4"
      />
      <Ellipse cx="111" cy="67" rx="47" ry="20" fill="rgba(248,202,132,0.1)" transform="rotate(-13 111 67)" />
      <G opacity={0.3} stroke="#2B1307" fill="none" strokeWidth="1.25">
        <Path d="M68 83 Q99 59 126 70" />
        <Path d="M104 52 Q141 39 178 53" />
        <Path d="M158 66 Q199 63 222 91" />
        <Path d="M61 137 Q94 116 121 126" />
        <Path d="M168 126 Q201 113 226 137" />
      </G>
      <Path
        d="M72 113 C103 92 151 84 204 91 C180 97 151 108 126 123 C104 136 88 143 76 143 C82 133 80 123 72 113 Z"
        fill="#170A04"
        stroke="#9A5828"
        strokeWidth="2.2"
      />
      <Path d="M86 116 C120 99 158 92 190 94" fill="none" stroke="rgba(226,165,96,0.2)" strokeWidth="1.5" />
      <Path d="M190 69 Q216 74 229 98 Q216 98 205 109" fill="none" stroke="#BA7133" strokeWidth="3.2" strokeLinecap="round" />
      <Ellipse cx="199" cy="80" rx="4" ry="3.5" fill="#170904" stroke="#D29753" strokeWidth="1.2" />
      <Path d="M123 34 Q141 15 159 34 Q150 45 123 34 Z" fill="#54270F" stroke="#A9622C" strokeWidth="1.5" />
    </Svg>
  );
}

function MokugyoMallet() {
  return (
    <Svg width={170} height={72} viewBox="0 0 170 72">
      <Defs>
        <RadialGradient id="clothHead" cx="0.35" cy="0.25" r="0.8">
          <Stop offset="0" stopColor="#F0DFC0" />
          <Stop offset="0.55" stopColor="#B89665" />
          <Stop offset="1" stopColor="#74502E" />
        </RadialGradient>
        <SvgLinearGradient id="malletHandle" x1="0" y1="0" x2="1" y2="0">
          <Stop offset="0" stopColor="#966038" />
          <Stop offset="1" stopColor="#4A2915" />
        </SvgLinearGradient>
      </Defs>
      <Path d="M42 31 L154 27 Q163 27 164 34 Q164 41 155 42 L42 41 Z" fill="url(#malletHandle)" stroke="rgba(225,176,118,0.22)" strokeWidth="1" />
      <Path
        d="M10 36 C10 15 49 10 58 29 C65 47 48 61 29 59 C17 58 10 49 10 36 Z"
        fill="url(#clothHead)"
        stroke="rgba(244,230,200,0.45)"
        strokeWidth="1.2"
      />
      <Path d="M18 27 Q35 18 51 28" stroke="rgba(80,50,24,0.26)" strokeWidth="1.2" fill="none" />
      <Path d="M17 40 Q35 31 54 40" stroke="rgba(80,50,24,0.2)" strokeWidth="1" fill="none" />
      <Ellipse cx="31" cy="24" rx="12" ry="5" fill="rgba(255,249,226,0.3)" transform="rotate(-12 31 24)" />
    </Svg>
  );
}

function createStyles() {
  return StyleSheet.create({
    root: {
      flex: 1,
      backgroundColor: "#060908",
    },
    modePager: {
      ...StyleSheet.absoluteFill,
    },
    page: {
      flex: 1,
      overflow: "hidden",
    },
    fullStage: {
      flex: 1,
      alignItems: "center",
      justifyContent: "center",
    },
    chrome: {
      ...StyleSheet.absoluteFill,
      justifyContent: "space-between",
    },
    topBar: {
      minHeight: 52,
      paddingHorizontal: 16,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
    },
    modeIndicator: {
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
    },
    modeDotHit: {
      padding: 6,
    },
    modeDot: {
      width: 5,
      height: 5,
      borderRadius: 2.5,
      backgroundColor: "rgba(244,241,232,0.18)",
    },
    modeDotActive: {
      width: 7,
      height: 7,
      borderRadius: 3.5,
      backgroundColor: "rgba(244,241,232,0.5)",
    },
    closeButton: {
      width: 40,
      height: 40,
      borderRadius: 20,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: "rgba(255,255,255,0.06)",
    },
    breathStage: {
      width: 260,
      height: 260,
      alignItems: "center",
      justifyContent: "center",
    },
    meditationHaze: {
      position: "absolute",
      left: -40,
      right: -40,
      top: "42%",
      height: 150,
      backgroundColor: "rgba(224,220,204,0.18)",
      borderRadius: 90,
      transform: [{ rotate: "-4deg" }],
    },
    mountainFar: {
      position: "absolute",
      left: -90,
      bottom: 150,
      width: 360,
      height: 300,
      borderRadius: 80,
      backgroundColor: "rgba(59,75,73,0.52)",
      transform: [{ rotate: "38deg" }],
    },
    mountainNear: {
      position: "absolute",
      right: -130,
      bottom: 80,
      width: 420,
      height: 380,
      borderRadius: 100,
      backgroundColor: "rgba(26,40,39,0.76)",
      transform: [{ rotate: "-34deg" }],
    },
    meditationWater: {
      position: "absolute",
      left: 0,
      right: 0,
      bottom: 0,
      height: "31%",
      backgroundColor: "rgba(16,30,31,0.62)",
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: "rgba(229,218,191,0.22)",
    },
    breathGlow: {
      position: "absolute",
      width: 180,
      height: 180,
      borderRadius: 90,
      backgroundColor: "rgba(255,225,169,0.34)",
    },
    breathCore: {
      width: 82,
      height: 82,
      borderRadius: 41,
      backgroundColor: "rgba(255,236,196,0.78)",
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: "rgba(255,249,229,0.68)",
    },
    mokugyoStage: {
      width: 300,
      height: 280,
      alignItems: "center",
      justifyContent: "center",
    },
    malletWrap: {
      position: "absolute",
      top: 34,
      right: -4,
      zIndex: 2,
      transformOrigin: "156px 35px",
    },
    strikePop: {
      position: "absolute",
      top: 86,
      left: 118,
      color: "rgba(246,220,171,0.9)",
      fontSize: 21,
      lineHeight: 26,
      fontWeight: "600",
      letterSpacing: 0.4,
    },
    windowRoom: {
      ...StyleSheet.absoluteFill,
      backgroundColor: "#050607",
    },
    windowVideo: {
      ...StyleSheet.absoluteFill,
      backgroundColor: "#070808",
    },
    windowGlass: {
      ...StyleSheet.absoluteFill,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: "rgba(255,255,255,0.16)",
      backgroundColor: "rgba(201,220,219,0.025)",
    },
    windowLoading: {
      ...StyleSheet.absoluteFill,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: "rgba(7,8,8,0.72)",
    },
    windowNetworkError: {
      ...StyleSheet.absoluteFill,
      alignItems: "center",
      justifyContent: "center",
      gap: 10,
      backgroundColor: "rgba(7,8,8,0.84)",
    },
    windowNetworkErrorText: {
      color: "rgba(244,241,232,0.5)",
      fontSize: 12,
      letterSpacing: 0.2,
    },
    windowLoadingDot: {
      width: 7,
      height: 7,
      borderRadius: 3.5,
      backgroundColor: "rgba(244,241,232,0.56)",
    },
    creditLine: {
      position: "absolute",
      left: 20,
      right: 20,
      bottom: 88,
      color: "rgba(244,241,232,0.55)",
      fontSize: 11,
      lineHeight: 15,
      letterSpacing: 0.2,
      textAlign: "center",
    },
    transientControls: {
      position: "absolute",
      left: 0,
      right: 0,
      bottom: 36,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "center",
      gap: 14,
    },
    ghostButton: {
      width: 44,
      height: 44,
      borderRadius: 22,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: "rgba(0,0,0,0.28)",
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: "rgba(255,255,255,0.18)",
    },
  });
}
