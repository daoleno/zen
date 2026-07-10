import React, { useEffect } from "react";
import {
  KeyboardAvoidingView,
  Modal,
  Platform,
  StyleSheet,
  Pressable,
} from "react-native";
import Animated, {
  useSharedValue,
  useAnimatedStyle,
  withSpring,
  withTiming,
  Easing,
} from "react-native-reanimated";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Spring } from "../../constants/motion";
import { TerminalRenameCard } from "./TerminalRenameCard";

const AnimatedPressable = Animated.createAnimatedComponent(Pressable);

interface TerminalRenameModalProps {
  visible: boolean;
  draft: string;
  placeholder: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onDraftChange(value: string): void;
  onClose(): void;
  onSave(): void;
}

export function TerminalRenameModal({
  visible,
  draft,
  placeholder,
  chrome,
  theme,
  onDraftChange,
  onClose,
  onSave,
}: TerminalRenameModalProps) {
  const progress = useSharedValue(0);

  useEffect(() => {
    if (visible) {
      progress.value = withSpring(1, Spring.rise);
    } else {
      progress.value = withTiming(0, { duration: 160, easing: Easing.out(Easing.ease) });
    }
  }, [visible, progress]);

  const backdropStyle = useAnimatedStyle(() => ({ opacity: progress.value }));
  const cardAnimStyle = useAnimatedStyle(() => ({
    transform: [{ translateY: (1 - progress.value) * 18 }],
    opacity: progress.value,
  }));

  return (
    <Modal visible={visible} transparent animationType="none" onRequestClose={onClose}>
      <KeyboardAvoidingView
        style={styles.renameRoot}
        behavior={Platform.OS === "ios" ? "padding" : "height"}
      >
        <AnimatedPressable
          style={[
            styles.modalBackdrop,
            { backgroundColor: chrome.overlay },
            backdropStyle,
          ]}
          onPress={onClose}
        />

        <Animated.View style={cardAnimStyle}>
          <TerminalRenameCard
            draft={draft}
            placeholder={placeholder}
            chrome={chrome}
            theme={theme}
            onDraftChange={onDraftChange}
            onClose={onClose}
            onSave={onSave}
          />
        </Animated.View>
      </KeyboardAvoidingView>
    </Modal>
  );
}

const styles = StyleSheet.create({
  renameRoot: {
    flex: 1,
    justifyContent: "center",
    paddingHorizontal: 20,
  },
  modalBackdrop: {
    ...StyleSheet.absoluteFill,
  },
});
