import React from 'react';
import {
  StyleSheet,
  TouchableOpacity,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { useAppColors } from '../../constants/tokens';
import { AppText } from '../ui';
import { NewTerminalCwdRow } from './NewTerminalCwdRow';
import {
  NewTerminalLaunchPresetList,
  type NewTerminalLaunchPreset,
} from './NewTerminalLaunchPresetList';
import {
  NewTerminalServerSelector,
  type NewTerminalServerOption,
} from './NewTerminalServerSelector';

export type { NewTerminalServerOption } from './NewTerminalServerSelector';
export type { NewTerminalLaunchPreset } from './NewTerminalLaunchPresetList';

interface NewTerminalQuickLaunchSectionProps {
  serverOptions: NewTerminalServerOption[];
  selectedServerId?: string | null;
  command: string;
  cwd: string;
  submitting: boolean;
  canSubmit: boolean;
  advanced: boolean;
  canPickDirectory: boolean;
  onSelectServer?(serverId: string): void;
  onPresetPress(preset: NewTerminalLaunchPreset): void;
  onToggleAdvanced(): void;
  onPickDirectory(): void;
}

export function NewTerminalQuickLaunchSection({
  serverOptions,
  selectedServerId,
  command,
  cwd,
  submitting,
  canSubmit,
  advanced,
  canPickDirectory,
  onSelectServer,
  onPresetPress,
  onToggleAdvanced,
  onPickDirectory,
}: NewTerminalQuickLaunchSectionProps) {
  const colors = useAppColors();

  return (
    <>
      <NewTerminalServerSelector
        serverOptions={serverOptions}
        selectedServerId={selectedServerId}
        onSelectServer={onSelectServer}
      />

      <NewTerminalCwdRow
        cwd={cwd}
        canPickDirectory={canPickDirectory}
        onPickDirectory={onPickDirectory}
      />

      <NewTerminalLaunchPresetList
        command={command}
        submitting={submitting}
        canSubmit={canSubmit}
        onPresetPress={onPresetPress}
      />

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

const styles = StyleSheet.create({
  advancedToggle: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    marginTop: 8,
    paddingVertical: 6,
  },
});
