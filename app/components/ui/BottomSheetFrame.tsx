import React, { useCallback, useEffect, useMemo } from "react";
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
  runOnJS,
  useSharedValue,
  useAnimatedStyle,
  withSpring,
  withTiming,
  Easing,
} from "react-native-reanimated";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
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
  dragToDismiss?: boolean;
  showHandle?: boolean;
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
  dragToDismiss = false,
  showHandle = true,
  onClose,
}: BottomSheetFrameProps) {
  const colors = useAppColors();
  const progress = useSharedValue(0);
  const dragY = useSharedValue(0);

  useEffect(() => {
    if (visible) {
      dragY.value = 0;
      progress.value = withSpring(1, Spring.rise);
    } else {
      progress.value = withTiming(0, {
        duration: 180,
        easing: Easing.out(Easing.ease),
      });
    }
  }, [dragY, visible, progress]);

  const backdropStyle = useAnimatedStyle(() => ({
    opacity: progress.value,
  }));

  const cardStyleAnim = useAnimatedStyle(() => ({
    transform: [{ translateY: (1 - progress.value) * 24 + dragY.value }],
    opacity: progress.value,
  }));

  const finishDragClose = useCallback(() => onClose(), [onClose]);
  const dragGesture = useMemo(
    () =>
      Gesture.Pan()
        .enabled(dragToDismiss)
        .activeOffsetY(10)
        .failOffsetX([-24, 24])
        .onUpdate((event) => {
          dragY.value = Math.max(0, event.translationY);
        })
        .onEnd((event) => {
          if (dragY.value > 96 || event.velocityY > 900) {
            runOnJS(finishDragClose)();
            return;
          }
          dragY.value = withSpring(0, Spring.rise);
        })
        .onFinalize(() => {
          if (dragY.value <= 96) {
            dragY.value = withSpring(0, Spring.rise);
          }
        }),
    [dragToDismiss, dragY, finishDragClose],
  );
  const handle = !showHandle ? null : dragToDismiss ? (
    <GestureDetector gesture={dragGesture}>
      <View style={styles.dragHandleTarget}>
        <View
          style={[
            styles.handle,
            styles.dragHandle,
            { backgroundColor: colors.borderStrong },
          ]}
        />
      </View>
    </GestureDetector>
  ) : (
    <View style={[styles.handle, { backgroundColor: colors.borderStrong }]} />
  );

  const card = (
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
      {handle}
      <View style={contentStyle}>{children}</View>
    </Animated.View>
  );
  const body = (
    <>
      <AnimatedPressable
        style={[
          styles.backdrop,
          { backgroundColor: colors.modalBackdrop },
          backdropStyle,
        ]}
        onPress={onClose}
      />
      {card}
    </>
  );

  return (
    <Modal
      visible={visible}
      transparent
      animationType="none"
      onRequestClose={onClose}
    >
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
  dragHandleTarget: {
    height: 18,
    alignItems: "center",
  },
  dragHandle: {
    marginBottom: 0,
  },
});
