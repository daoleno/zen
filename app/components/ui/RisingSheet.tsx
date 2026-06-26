import React, { useEffect } from "react";
import {
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  StyleSheet,
  View,
  type StyleProp,
  type ViewStyle,
} from "react-native";
import Animated, {
  useSharedValue,
  useAnimatedStyle,
  withSpring,
  withTiming,
  Easing,
  interpolate,
} from "react-native-reanimated";
import { Spring } from "../../constants/motion";
import { useAppColors } from "../../constants/tokens";

const AnimatedPressable = Animated.createAnimatedComponent(Pressable);

/**
 * A reusable centered overlay: the dimmed backdrop fades in while the card
 * rises with a calm spring — the native "dialog appears" feel. Used by context
 * menus, rename dialogs, and any centered overlay so they share one entrance.
 *
 * Manages its own Modal; `visible` drives a spring-in and a timed fade-out.
 * Set `avoidKeyboard` for the rename-style dialog that must lift above the
 * keyboard.
 */
export interface RisingSheetProps {
  visible: boolean;
  onClose: () => void;
  cardStyle?: StyleProp<ViewStyle>;
  /** Vertical travel in px for the rise. Default 18. */
  rise?: number;
  align?: "center" | "bottom";
  avoidKeyboard?: boolean;
  children?: React.ReactNode;
}

export function RisingSheet({
  visible,
  onClose,
  cardStyle,
  rise = 18,
  align = "center",
  avoidKeyboard = false,
  children,
}: RisingSheetProps) {
  const colors = useAppColors();
  const progress = useSharedValue(0);

  useEffect(() => {
    if (visible) {
      progress.value = 0;
      progress.value = withSpring(1, Spring.rise);
    } else {
      progress.value = withTiming(0, { duration: 160, easing: Easing.out(Easing.ease) });
    }
  }, [visible, progress]);

  const backdropStyle = useAnimatedStyle(() => ({ opacity: progress.value }));

  const cardAnimStyle = useAnimatedStyle(() => ({
    opacity: interpolate(progress.value, [0, 0.5, 1], [0, 0.5, 1]),
    transform: [{ translateY: interpolate(progress.value, [0, 1], [rise, 0]) }],
  }));

  const alignStyle = align === "bottom" ? styles.bottom : styles.center;

  const renderInner = () => (
    <View style={styles.root}>
      <AnimatedPressable style={[styles.backdrop, { backgroundColor: colors.modalBackdrop }, backdropStyle]} onPress={onClose} />
      <View style={[styles.cardSlot, alignStyle]}>
        <Animated.View style={[cardAnimStyle, cardStyle]}>
          {children}
        </Animated.View>
      </View>
    </View>
  );

  return (
    <Modal visible={visible} transparent animationType="none" onRequestClose={onClose}>
      {avoidKeyboard ? (
        <KeyboardAvoidingView
          style={styles.root}
          behavior={Platform.OS === "ios" ? "padding" : "height"}
        >
          <AnimatedPressable style={[styles.backdrop, { backgroundColor: colors.modalBackdrop }, backdropStyle]} onPress={onClose} />
          <View style={[styles.cardSlot, alignStyle]}>
            <Animated.View style={[cardAnimStyle, cardStyle]}>
              {children}
            </Animated.View>
          </View>
        </KeyboardAvoidingView>
      ) : (
        renderInner()
      )}
    </Modal>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
  },
  backdrop: {
    ...StyleSheet.absoluteFill,
  },
  cardSlot: {
    flex: 1,
    paddingHorizontal: 12,
  },
  center: {
    justifyContent: "center",
  },
  bottom: {
    justifyContent: "flex-end",
    paddingBottom: 32,
  },
});
