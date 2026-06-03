import React, { useMemo } from "react";
import {
  StyleSheet,
  TouchableOpacity,
  View,
} from "react-native";
import { Colors, useAppColors } from "../../constants/tokens";
import type { AgentKind } from "../../services/agentPresentation";
import {
  CLAUDE_CODE_COMMAND,
  CODEX_COMMAND,
} from "../../services/agentCommands";
import { AgentKindIcon } from "./AgentKindIcon";
import { AppText } from "../ui";

export type NewTerminalLaunchPreset = {
  key: string;
  kind: AgentKind;
  label: string;
  command: string;
};

const LAUNCH_PRESETS: readonly NewTerminalLaunchPreset[] = [
  { key: "shell", kind: "terminal", label: "Shell", command: "" },
  {
    key: "claude",
    kind: "claude",
    label: "Claude",
    command: CLAUDE_CODE_COMMAND,
  },
  { key: "codex", kind: "codex", label: "Codex", command: CODEX_COMMAND },
];

interface NewTerminalLaunchPresetListProps {
  command: string;
  submitting: boolean;
  canSubmit: boolean;
  onPresetPress(preset: NewTerminalLaunchPreset): void;
}

export function NewTerminalLaunchPresetList({
  command,
  submitting,
  canSubmit,
  onPresetPress,
}: NewTerminalLaunchPresetListProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const activePreset = useMemo(
    () => LAUNCH_PRESETS.find((preset) => preset.command === command.trim())?.key ?? null,
    [command],
  );

  return (
    <View style={styles.presetGrid}>
      {LAUNCH_PRESETS.map((preset) => {
        const active = activePreset === preset.key;
        return (
          <TouchableOpacity
            key={preset.key}
            style={[
              styles.presetCard,
              active && styles.presetCardActive,
              submitting && styles.presetCardDisabled,
            ]}
            onPress={() => onPresetPress(preset)}
            disabled={!canSubmit}
            activeOpacity={0.82}
          >
            <View style={styles.presetIcon}>
              <AgentKindIcon kind={preset.kind} size={20} />
            </View>
            <AppText
              variant="label"
              tone={active ? "primary" : "secondary"}
              style={styles.presetLabel}
            >
              {preset.label}
            </AppText>
          </TouchableOpacity>
        );
      })}
    </View>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    presetGrid: {
      flexDirection: "row",
      gap: 10,
    },
    presetCard: {
      flex: 1,
      minHeight: 88,
      alignItems: "center",
      justifyContent: "center",
      gap: 10,
      paddingHorizontal: 8,
      paddingVertical: 12,
      borderRadius: 14,
      backgroundColor: colors.surfaceSubtle,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
    },
    presetCardActive: {
      backgroundColor: colors.surfaceActive,
      borderColor: colors.accent,
    },
    presetCardDisabled: {
      opacity: 0.5,
    },
    presetIcon: {
      alignItems: "center",
      justifyContent: "center",
    },
    presetLabel: {
      textAlign: "center",
    },
  });
}