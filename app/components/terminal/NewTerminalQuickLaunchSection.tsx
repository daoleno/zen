import React, { useMemo } from 'react';
import {
  StyleSheet,
  TouchableOpacity,
  View,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { Colors, useAppColors } from '../../constants/tokens';
import type { AgentKind } from '../../services/agentPresentation';
import { CLAUDE_CODE_COMMAND, CODEX_COMMAND } from '../../services/agentCommands';
import { AgentKindIcon } from './AgentKindIcon';
import { AppText } from '../ui';
import {
  NewTerminalServerSelector,
  type NewTerminalServerOption,
} from './NewTerminalServerSelector';

export type { NewTerminalServerOption } from './NewTerminalServerSelector';

export type NewTerminalLaunchPreset = {
  key: string;
  kind: AgentKind;
  label: string;
  command: string;
};

const LAUNCH_PRESETS: readonly NewTerminalLaunchPreset[] = [
  { key: 'shell', kind: 'terminal', label: 'Shell', command: '' },
  { key: 'claude', kind: 'claude', label: 'Claude', command: CLAUDE_CODE_COMMAND },
  { key: 'codex', kind: 'codex', label: 'Codex', command: CODEX_COMMAND },
];

interface NewTerminalQuickLaunchSectionProps {
  serverOptions: NewTerminalServerOption[];
  selectedServerId?: string | null;
  command: string;
  submitting: boolean;
  canSubmit: boolean;
  advanced: boolean;
  onSelectServer?(serverId: string): void;
  onPresetPress(preset: NewTerminalLaunchPreset): void;
  onToggleAdvanced(): void;
}

export function NewTerminalQuickLaunchSection({
  serverOptions,
  selectedServerId,
  command,
  submitting,
  canSubmit,
  advanced,
  onSelectServer,
  onPresetPress,
  onToggleAdvanced,
}: NewTerminalQuickLaunchSectionProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const activePreset = useMemo(
    () => LAUNCH_PRESETS.find(preset => preset.command === command.trim())?.key ?? null,
    [command],
  );

  return (
    <>
      <NewTerminalServerSelector
        serverOptions={serverOptions}
        selectedServerId={selectedServerId}
        onSelectServer={onSelectServer}
      />

      <View style={styles.presetList}>
        {LAUNCH_PRESETS.map(preset => (
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

      <TouchableOpacity
        style={styles.advancedToggle}
        onPress={onToggleAdvanced}
        activeOpacity={0.82}
      >
        <Ionicons
          name={advanced ? 'chevron-down' : 'chevron-forward'}
          size={14}
          color={colors.textSecondary}
        />
        <AppText variant="caption" tone="secondary">
          Advanced
        </AppText>
      </TouchableOpacity>
    </>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    presetList: {
      gap: 0,
    },
    presetCard: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 12,
      minHeight: 42,
      paddingVertical: 8,
      paddingHorizontal: 4,
      backgroundColor: 'transparent',
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
    advancedToggle: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 6,
      marginTop: 8,
      paddingVertical: 6,
    },
  });
}
