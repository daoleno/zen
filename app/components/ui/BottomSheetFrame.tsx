import React, { useEffect } from "react";
import {
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  type StyleProp,
  StyleSheet,
  View,
  type ViewStyle,
} from "react-native";
import Animated, {
  useSharedValue,
  useAnimatedStyle,
  withSpring,
  withTiming,
  Easing,
} from "react-native-reanimated";
import { useAppColors } from "../../constants/tokens";
import { Spring } from "../../constants/motion";

const AnimatedPressable = Animated.createAnimatedComponent(Pressable);

interface BottomSheetFrameProps {
  visible: boolean;
  children: React.ReactNode;
  maxHeight?: `${number}%` | number;
  rootStyle?: StyleProp<ViewStyle>;
  cardStyle?: StyleProp<ViewStyle>;
  contentStyle?: StyleProp<ViewStyle>;
  keyboardAvoiding?: boolean;
  onClose(): void;
}

/**
 * The shared bottom-sheet primitive used across the app (Brain executor sheet,
 * new-terminal sheet, agent picker, etc.). It fades the backdrop and springs
 * the card up from the bottom on open — one consistent gesture everywhere,
 * matching the RisingSheet language used by centered dialogs.
 */
export function BottomSheetFrame({
  visible,
  children,
  maxHeight = "75%",
  rootStyle,
  cardStyle,
  contentStyle,
  keyboardAvoiding = false,
  onClose,
}: BottomSheetFrameProps) {
  const colors = useAppColors();
  const progress = useSharedValue(0);

  useEffect(() => {
    if (visible) {
      progress.value = withSpring(1, Spring.rise);
    } else {
      progress.value = withTiming(0, { duration: 180, easing: Easing.out(Easing.ease) });
    }
  }, [visible, progress]);

  const backdropStyle = useAnimatedStyle(() => ({
    opacity: progress.value,
  }));

  const cardStyleAnim = useAnimatedStyle(() => ({
    transform: [{ translateY: (1 - progress.value) * 24 }],
    opacity: progress.value,
  }));

  const body = (
    <>
      <AnimatedPressable
        style={[styles.backdrop, { backgroundColor: colors.modalBackdrop }, backdropStyle]}
        onPress={onClose}
      />
      <Animated.View
        style={[
          styles.card,
          {
            maxHeight,
            backgroundColor: colors.modalSurface,
            borderColor: colors.borderSubtle,
          },
          cardStyleAnim,
          cardStyle,
        ]}
      >
        <View style={[styles.handle, { backgroundColor: colors.borderStrong }]} />
        <View style={contentStyle}>{children}</View>
      </Animated.View>
    </>
  );

  return (
    <Modal visible={visible} transparent animationType="none" onRequestClose={onClose}>
      {keyboardAvoiding ? (
        <KeyboardAvoidingView
          style={[styles.root, rootStyle]}
          behavior={Platform.OS === "ios" ? "padding" : "height"}
        >
          {body}
        </KeyboardAvoidingView>
      ) : (
        <View style={[styles.root, rootStyle]}>{body}</View>
      )}
    </Modal>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
    justifyContent: "flex-end",
  },
  backdrop: {
    ...StyleSheet.absoluteFill,
  },
  card: {
    borderTopLeftRadius: 28,
    borderTopRightRadius: 28,
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 18,
    paddingTop: 12,
    paddingBottom: 28,
  },
  handle: {
    alignSelf: "center",
    width: 42,
    height: 4,
    borderRadius: 2,
    marginBottom: 14,
  },
});
