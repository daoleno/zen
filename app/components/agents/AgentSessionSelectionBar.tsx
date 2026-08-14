import React, { useMemo } from "react";
import { StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import {
  TypeScale,
  UiTextMetrics,
  useAppColors,
  type AppColors,
} from "../../constants/tokens";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { selectionCountLabel } from "../../services/sessionSelection";

export interface AgentSessionSelectionBarProps {
  count: number;
  /** True while a termination batch is in flight (submit + acknowledgement). */
  terminating: boolean;
  onCancel(): void;
  onTerminate(): void;
}

/**
 * Telegram-style selection chrome for the Sessions list: Cancel, selected
 * count and the primary destructive Terminate action. Rendered inside the
 * drawer shell's app bar slot while selection mode is active.
 */
export function AgentSessionSelectionBar({
  count,
  terminating,
  onCancel,
  onTerminate,
}: AgentSessionSelectionBarProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const cancelDisabled = terminating;
  const terminateDisabled = terminating || count === 0;

  return (
    <View style={styles.container}>
      <AnimatedPressable
        style={styles.sideButton}
        preset="press"
        scale={0.96}
        disabled={cancelDisabled}
        onPress={onCancel}
        accessibilityRole="button"
        accessibilityLabel="Cancel selection"
        accessibilityHint="Exits selection mode without changing sessions"
        accessibilityState={{ disabled: cancelDisabled }}
        hitSlop={6}
      >
        <Text style={styles.cancelText}>Cancel</Text>
      </AnimatedPressable>

      <View style={styles.countWrap}>
        <Ionicons
          name="checkmark-circle"
          size={15}
          color={colors.accent}
          style={styles.countIcon}
        />
        <Text
          style={styles.countText}
          numberOfLines={1}
          accessibilityLiveRegion="polite"
        >
          {selectionCountLabel(count)}
        </Text>
      </View>

      <AnimatedPressable
        style={styles.sideButton}
        preset="press"
        scale={0.96}
        disabled={terminateDisabled}
        onPress={onTerminate}
        accessibilityRole="button"
        accessibilityLabel="Terminate selected sessions"
        accessibilityHint="Terminates every selected session after one confirmation"
        accessibilityState={{
          disabled: terminateDisabled,
          busy: terminating,
        }}
        hitSlop={6}
      >
        <Text
          style={[
            styles.terminateText,
            terminateDisabled && styles.terminateTextDisabled,
          ]}
        >
          {terminating ? "Terminating…" : "Terminate"}
        </Text>
      </AnimatedPressable>
    </View>
  );
}

function createStyles(colors: AppColors) {
  return StyleSheet.create({
    container: {
      flex: 1,
      alignSelf: "stretch",
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      paddingHorizontal: 12,
    },
    sideButton: {
      minWidth: 88,
      minHeight: 44,
      alignItems: "center",
      justifyContent: "center",
    },
    countWrap: {
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
      flexShrink: 1,
      minWidth: 0,
      paddingHorizontal: 8,
    },
    countIcon: {
      marginTop: 1,
    },
    countText: {
      ...UiTextMetrics,
      ...TypeScale.body,
      color: colors.textPrimary,
      flexShrink: 1,
    },
    cancelText: {
      ...UiTextMetrics,
      ...TypeScale.body,
      color: colors.textPrimary,
    },
    terminateText: {
      ...UiTextMetrics,
      ...TypeScale.body,
      color: colors.dangerText,
    },
    terminateTextDisabled: {
      color: colors.disabledText,
    },
  });
}
