import React from "react";
import { StyleSheet, TouchableOpacity, View, Text } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { ContinuousCorners, TypeScale, shadow } from "../../constants/tokens";
import { composerLitEdge } from "./composerMaterial";

interface InterfaceTimelineJumpButtonProps {
  bottom: number;
  chrome: TerminalThemeChrome;
  label?: string;
  onPress(): void;
}

/**
 * Floating jump-to-latest control in the same glass material as the
 * Composer capsule: a 44 pt circle, widening into a capsule when it carries
 * the time since the newest message.
 */
export function InterfaceTimelineJumpButton({
  bottom,
  chrome,
  label,
  onPress,
}: InterfaceTimelineJumpButtonProps) {
  return (
    <TouchableOpacity
      accessibilityLabel={
        label ? `Jump to latest, ${label} ago` : "Jump to latest"
      }
      accessibilityRole="button"
      style={[
        styles.jumpButton,
        label ? styles.jumpButtonWithLabel : null,
        {
          backgroundColor: chrome.composerInput,
          borderColor: chrome.border,
          bottom,
          ...shadow("raised", chrome.shadowColor),
        },
      ]}
      onPress={onPress}
      activeOpacity={0.72}
    >
      {label ? (
        // Only the capsule has a straight top run for the lit edge; on the
        // bare circle any hairline would overhang the curve.
        <View
          pointerEvents="none"
          style={[styles.litEdge, { backgroundColor: composerLitEdge(chrome) }]}
        />
      ) : null}
      <View style={styles.content}>
        <Ionicons name="arrow-down" size={18} color={chrome.text} />
        {label ? (
          <Text
            numberOfLines={1}
            maxFontSizeMultiplier={1.6}
            style={[styles.label, { color: chrome.textMuted }]}
          >
            {label}
          </Text>
        ) : null}
      </View>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  jumpButton: {
    position: "absolute",
    alignItems: "center",
    justifyContent: "center",
    width: 44,
    height: 44,
    borderRadius: 22,
    ...ContinuousCorners,
    borderWidth: StyleSheet.hairlineWidth,
    right: 16,
    zIndex: 4,
  },
  jumpButtonWithLabel: {
    width: "auto",
    paddingLeft: 12,
    paddingRight: 14,
  },
  litEdge: {
    position: "absolute",
    top: 0,
    left: 22,
    right: 22,
    height: StyleSheet.hairlineWidth,
  },
  content: {
    alignItems: "center",
    flexDirection: "row",
  },
  label: {
    ...TypeScale.caption,
    marginLeft: 6,
  },
});
