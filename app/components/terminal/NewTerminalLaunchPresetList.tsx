import React, { useMemo } from "react";
import {
  StyleSheet,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
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
    <View style={styles.presetList}>
      {LAUNCH_PRESETS.map((preset) => (
        <TouchableOpacity
          key={preset.key}
          style={[
            styles.presetCard,
            activePreset === preset.key && styles.presetCardActive,
            submitting && styles.presetCardDisabled,
          ]}
          onPress={() => onPresetPress(preset)}
          disabled={!canSubmit}
          activeOpacity={0.78}
        >
          <AgentKindIcon kind={preset.kind} size={18} />
          <View style={styles.presetCardText}>
            <AppText variant="button">{preset.label}</AppText>
          </View>
          <Ionicons name="chevron-forward" size={14} color={colors.textSecondary} />
        </TouchableOpacity>
      ))}
    </View>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    presetList: {
      gap: 0,
    },
    presetCard: {
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      minHeight: 42,
      paddingVertical: 8,
      paddingHorizontal: 4,
      backgroundColor: "transparent",
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    presetCardActive: {
      backgroundColor: colors.surfaceActive,
    },
    presetCardDisabled: {
      opacity: 0.5,
    },
    presetCardText: {
      flex: 1,
    },
  });
}
