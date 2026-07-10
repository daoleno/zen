import React, { useEffect } from "react";
import {
  Modal,
  StyleSheet,
  Text,
  Pressable,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import Animated, {
  useSharedValue,
  useAnimatedStyle,
  withSpring,
  withTiming,
  Easing,
} from "react-native-reanimated";
import { TypeScale } from "../../constants/tokens";
import { Spring } from "../../constants/motion";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { AgentDirectorySection } from "../../services/serverSelection";
import { TerminalAgentPickerList } from "./TerminalAgentPickerList";
import { AnimatedPressable } from "../ui/AnimatedPressable";

const AnimatedPressableBackdrop = Animated.createAnimatedComponent(Pressable);

interface TerminalAgentPickerSheetProps {
  visible: boolean;
  sections: AgentDirectorySection[];
  agentCount: number;
  activeSessionKey: string | null;
  showServerNames: boolean;
  agentAliases: Record<string, string | undefined>;
  creatingSession: boolean;
  chrome: TerminalThemeChrome;
  onClose(): void;
  onOpenAgent(sessionKey: string): void;
  onNewTerminal(): void;
}

export function TerminalAgentPickerSheet({
  visible,
  sections,
  agentCount,
  activeSessionKey,
  showServerNames,
  agentAliases,
  creatingSession,
  chrome,
  onClose,
  onOpenAgent,
  onNewTerminal,
}: TerminalAgentPickerSheetProps) {
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
    transform: [{ translateY: (1 - progress.value) * 24 }],
    opacity: progress.value,
  }));

  return (
    <Modal visible={visible} transparent animationType="none" onRequestClose={onClose}>
      <View style={styles.modalRoot}>
        <AnimatedPressableBackdrop
          style={[
            styles.modalBackdrop,
            { backgroundColor: chrome.overlay },
            backdropStyle,
          ]}
          onPress={onClose}
        />

        <Animated.View
          style={[
            styles.sheetCard,
            {
              backgroundColor: chrome.surface,
              borderColor: chrome.border,
            },
            cardAnimStyle,
          ]}
        >
          <View
            style={[styles.sheetHandle, { backgroundColor: chrome.textSubtle }]}
          />

          <TerminalAgentPickerList
            sections={sections}
            agentCount={agentCount}
            activeSessionKey={activeSessionKey}
            showServerNames={showServerNames}
            agentAliases={agentAliases}
            chrome={chrome}
            onOpenAgent={onOpenAgent}
          />

          <AnimatedPressable
            style={[
              styles.sheetCreateButton,
              {
                backgroundColor: creatingSession
                  ? chrome.disabledSurface
                  : chrome.surfaceMuted,
                borderColor: chrome.border,
              },
            ]}
            accessibilityRole="button"
            accessibilityLabel="New terminal"
            accessibilityState={{ disabled: creatingSession, busy: creatingSession }}
            preset="press"
            scale={0.97}
            disabled={creatingSession}
            onPress={() => {
              Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
              onNewTerminal();
            }}
          >
            <Ionicons name="add" size={16} color={chrome.textMuted} />
            <Text style={[styles.sheetCreateButtonText, { color: chrome.textMuted }]}>
              {creatingSession ? "Starting…" : "New Terminal"}
            </Text>
          </AnimatedPressable>
        </Animated.View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  modalRoot: {
    flex: 1,
    justifyContent: "flex-end",
  },
  modalBackdrop: {
    ...StyleSheet.absoluteFill,
  },
  sheetCard: {
    borderTopLeftRadius: 28,
    borderTopRightRadius: 28,
    paddingHorizontal: 18,
    paddingTop: 12,
    paddingBottom: 28,
    borderTopWidth: 1,
    maxHeight: "82%",
  },
  sheetHandle: {
    alignSelf: "center",
    width: 42,
    height: 4,
    borderRadius: 2,
    marginBottom: 14,
  },
  sheetCreateButton: {
    marginTop: 12,
    height: 44,
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    borderStyle: "dashed" as const,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
  },
  sheetCreateButtonText: {
    ...TypeScale.label,
  },
});
