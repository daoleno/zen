import React, { useMemo } from "react";
import { Ionicons } from "@expo/vector-icons";
import {
  StyleSheet,
  TouchableOpacity,
  View,
} from "react-native";
import { Colors, useAppColors } from "../../constants/tokens";
import type { AgentKind } from "../../services/workerPresentation";
import {
  CLAUDE_CODE_COMMAND,
  CODEX_COMMAND,
  CURSOR_AGENT_COMMAND,
  GROK_COMMAND,
  OPENCODE_COMMAND,
  PI_COMMAND,
  AMP_COMMAND,
} from "../../services/agentCommands";
import { AMP_ACCOUNT_STATUS, AMP_CAPABILITY_SUMMARY, AMP_LAUNCH_LIMITATION } from "../../services/ampAgent";
import { AgentKindIcon } from "./AgentKindIcon";
import { AppText } from "../ui";

export type NewTerminalLaunchPreset = {
  key: string;
  kind: AgentKind;
  label: string;
  command: string;
  unavailableReason?: string;
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
  { key: "cursor", kind: "cursor", label: "Cursor", command: CURSOR_AGENT_COMMAND },
  { key: "grok", kind: "grok", label: "Grok", command: GROK_COMMAND },
  { key: "pi", kind: "pi", label: "Pi", command: PI_COMMAND },
  { key: "opencode", kind: "opencode", label: "OpenCode", command: OPENCODE_COMMAND },
  { key: "amp", kind: "amp", label: "Amp", command: AMP_COMMAND, unavailableReason: AMP_LAUNCH_LIMITATION },
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
    <View>
      <View style={styles.presetGrid}>
      {LAUNCH_PRESETS.map((preset) => {
        const active = !preset.unavailableReason && activePreset === preset.key;
        const disabled = !canSubmit || Boolean(preset.unavailableReason);
        return (
          <TouchableOpacity
            key={preset.key}
            accessibilityRole="button"
            accessibilityLabel={preset.unavailableReason ? `${preset.label}. ${preset.unavailableReason}` : preset.label}
            accessibilityState={{ disabled, selected: active }}
            style={[
              styles.presetCard,
              preset.key === "shell" && styles.presetCardWide,
              active && styles.presetCardActive,
              (submitting || disabled) && styles.presetCardDisabled,
            ]}
            onPress={() => {
              if (!disabled) onPresetPress(preset);
            }}
            disabled={disabled}
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
            {preset.unavailableReason ? <Ionicons name="lock-closed-outline" size={14} color={colors.textSecondary} /> : null}
          </TouchableOpacity>
        );
      })}
      </View>
      <View style={styles.limitation}>
        <AppText variant="caption" tone="secondary">Amp: {AMP_LAUNCH_LIMITATION}</AppText>
        <AppText variant="caption" tone="secondary">{AMP_CAPABILITY_SUMMARY}</AppText>
        <AppText variant="caption" tone="secondary">{AMP_ACCOUNT_STATUS}</AppText>
      </View>
    </View>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    presetGrid: {
      flexDirection: "row",
      flexWrap: "wrap",
      gap: 10,
    },
    presetCard: {
      width: "48%",
      minHeight: 64,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "flex-start",
      gap: 12,
      paddingHorizontal: 14,
      paddingVertical: 10,
      borderRadius: 12,
      backgroundColor: colors.surfaceSubtle,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
    },
    presetCardActive: {
      backgroundColor: colors.surfaceActive,
      borderColor: colors.accent,
    },
    presetCardWide: {
      width: "100%",
    },
    presetCardDisabled: {
      opacity: 0.5,
    },
    presetIcon: {
      width: 28,
      flexShrink: 0,
      alignItems: "center",
      justifyContent: "center",
    },
    presetLabel: {
      flex: 1,
      minWidth: 0,
    },
    limitation: {
      marginTop: 10,
      gap: 4,
    },
  });
}
