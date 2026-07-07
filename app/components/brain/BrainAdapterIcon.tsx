import React, { useMemo } from "react";
import { Ionicons } from "@expo/vector-icons";
import { StyleSheet, View } from "react-native";
import { Claude, Codex, Grok } from "@lobehub/icons-rn";
import { Colors, useAppTheme } from "../../constants/tokens";
import type { BrainAdapterRef } from "../../store/brain";
import { brainAdapterProviderKey } from "./brainPresentation";
import { CursorMark } from "../icons/CursorMark";

interface BrainAdapterIconProps {
  adapter: BrainAdapterRef;
  size?: number;
}

export function BrainAdapterIcon({ adapter, size = 18 }: BrainAdapterIconProps) {
  const { colors, isLight } = useAppTheme();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const provider = brainAdapterProviderKey(adapter);
  const frameSize = size + 14;

  if (provider === "claude") {
    return <Claude.Color size={size} />;
  }

  if (provider === "codex") {
    return <Codex.Color size={size} />;
  }

  if (provider === "cursor") {
    return <CursorMark size={size} color={isLight ? "#000" : "#fff"} />;
  }

  if (provider === "grok") {
    return (
      <View style={[styles.frame, styles.grok, { width: frameSize, height: frameSize }]}>
        <Grok size={size} color={colors.textPrimary} />
      </View>
    );
  }

  return (
    <View style={[styles.frame, styles.custom, { width: frameSize, height: frameSize }]}>
      <Ionicons
        name={provider === "tmux" ? "terminal-outline" : "hardware-chip-outline"}
        size={size}
        color={colors.textSecondary}
      />
    </View>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    frame: {
      borderRadius: 12,
      alignItems: "center",
      justifyContent: "center",
      borderWidth: StyleSheet.hairlineWidth,
    },
    grok: {
      backgroundColor: colors.surfaceSubtle,
      borderColor: colors.borderSubtle,
    },
    custom: {
      backgroundColor: colors.surfaceSubtle,
      borderColor: colors.borderSubtle,
    },
  });
}
