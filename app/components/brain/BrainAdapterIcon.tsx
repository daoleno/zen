import React, { useMemo } from "react";
import { Ionicons } from "@expo/vector-icons";
import { StyleSheet, View } from "react-native";
import { Claude, OpenAI } from "@lobehub/icons-rn";
import { Colors, useAppColors } from "../../constants/tokens";
import type { BrainAdapterRef } from "../../store/brain";
import { brainAdapterProviderKey } from "./brainPresentation";

interface BrainAdapterIconProps {
  adapter: BrainAdapterRef;
  size?: number;
}

export function BrainAdapterIcon({ adapter, size = 18 }: BrainAdapterIconProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const provider = brainAdapterProviderKey(adapter);
  const frameSize = size + 14;

  if (provider === "claude") {
    return (
      <View style={[styles.frame, styles.claude, { width: frameSize, height: frameSize }]}>
        <Claude.Color size={size} />
      </View>
    );
  }

  if (provider === "codex") {
    return (
      <View style={[styles.frame, styles.codex, { width: frameSize, height: frameSize }]}>
        <OpenAI.Avatar size={size} />
      </View>
    );
  }

  if (provider === "grok") {
    return (
      <View style={[styles.frame, styles.custom, { width: frameSize, height: frameSize }]}>
        <Ionicons name="sparkles" size={size} color={colors.textSecondary} />
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
    codex: {
      backgroundColor: colors.accentSoft,
      borderColor: colors.borderSubtle,
    },
    claude: {
      backgroundColor: colors.surfaceSubtle,
      borderColor: colors.borderSubtle,
    },
    custom: {
      backgroundColor: colors.surfaceSubtle,
      borderColor: colors.borderSubtle,
    },
  });
}