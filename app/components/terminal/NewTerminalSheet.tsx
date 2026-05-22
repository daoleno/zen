import React, { useEffect, useMemo, useState } from 'react';
import {
  Keyboard,
  ScrollView,
  StyleSheet,
  TextInput,
  TouchableOpacity,
  View,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { Colors, Typography, useAppColors } from '../../constants/tokens';
import { AgentKindIcon } from './AgentKindIcon';
import { DirectoryPicker } from './DirectoryPicker';
import type { AgentKind } from '../../services/agentPresentation';
import { CLAUDE_CODE_COMMAND, CODEX_COMMAND } from '../../services/agentCommands';
import { AppButton, AppText, BottomSheetFrame, IconButton } from '../ui';

type ServerOption = {
  id: string;
  name: string;
};

type LaunchPreset = {
  key: string;
  kind: AgentKind;
  label: string;
  command: string;
};

const PRESETS: readonly LaunchPreset[] = [
  { key: 'shell', kind: 'terminal', label: 'Shell', command: '' },
  { key: 'claude', kind: 'claude', label: 'Claude', command: CLAUDE_CODE_COMMAND },
  { key: 'codex', kind: 'codex', label: 'Codex', command: CODEX_COMMAND },
];

interface NewTerminalSheetProps {
  visible: boolean;
  title: string;
  subtitle: string;
  initialCwd?: string;
  initialCommand?: string;
  initialName?: string;
  submitting?: boolean;
  serverOptions?: ServerOption[];
  selectedServerId?: string | null;
  onSelectServer?(serverId: string): void;
  onClose(): void;
  onSubmit(input: { cwd: string; command: string; name: string; serverId?: string }): void;
}

export function NewTerminalSheet({
  visible,
  title: _title,
  subtitle: _subtitle,
  initialCwd = '',
  initialCommand = '',
  initialName = '',
  submitting = false,
  serverOptions = [],
  selectedServerId,
  onSelectServer,
  onClose,
  onSubmit,
}: NewTerminalSheetProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const [cwd, setCwd] = useState(initialCwd);
  const [command, setCommand] = useState(initialCommand);
  const [name, setName] = useState(initialName);
  const [advanced, setAdvanced] = useState(false);
  const [dirPickerOpen, setDirPickerOpen] = useState(false);

  useEffect(() => {
    if (!visible) return;
    Keyboard.dismiss();
    setCwd(initialCwd);
    setCommand(initialCommand);
    setName(initialName);
    setAdvanced(false);
    setDirPickerOpen(false);
  }, [initialCommand, initialCwd, initialName, visible]);

  const canSubmit = !submitting && (!serverOptions.length || Boolean(selectedServerId));

  const activePreset = useMemo(
    () => PRESETS.find(p => p.command === command.trim())?.key ?? null,
    [command],
  );

  const handleClose = () => {
    Keyboard.dismiss();
    onClose();
  };

  const handlePresetTap = (preset: LaunchPreset) => {
    if (!canSubmit) return;
    Keyboard.dismiss();
    setCommand(preset.command);
    if (!advanced) {
      onSubmit({
        cwd: cwd.trim() || initialCwd.trim(),
        command: preset.command,
        name: '',
        serverId: selectedServerId ?? undefined,
      });
    }
  };

  const handleAdvancedSubmit = () => {
    if (!canSubmit) return;
    Keyboard.dismiss();
    onSubmit({
      cwd: cwd.trim(),
      command: command.trim(),
      name: name.trim(),
      serverId: selectedServerId ?? undefined,
    });
  };

  const sheetContent = (
    <BottomSheetFrame
      visible={visible}
      onClose={handleClose}
      maxHeight="68%"
      cardStyle={styles.sheetCard}
      keyboardAvoiding
    >
      <ScrollView
        showsVerticalScrollIndicator={false}
        keyboardShouldPersistTaps="always"
        keyboardDismissMode="none"
        bounces={false}
      >
        {serverOptions.length > 1 ? (
          <View style={styles.serverSection}>
            <ScrollView horizontal showsHorizontalScrollIndicator={false}>
              <View style={styles.serverRow}>
                {serverOptions.map(server => {
                  const active = server.id === selectedServerId;
                  return (
                    <TouchableOpacity
                      key={server.id}
                      style={[styles.serverChip, active && styles.serverChipActive]}
                      onPress={() => onSelectServer?.(server.id)}
                      activeOpacity={0.84}
                    >
                      <AppText variant="label" tone={active ? 'primary' : 'secondary'}>
                        {server.name}
                      </AppText>
                    </TouchableOpacity>
                  );
                })}
              </View>
            </ScrollView>
          </View>
        ) : null}

        <View style={styles.presetList}>
          {PRESETS.map(preset => (
            <TouchableOpacity
              key={preset.key}
              style={[
                styles.presetCard,
                activePreset === preset.key && styles.presetCardActive,
                submitting && styles.presetCardDisabled,
              ]}
              onPress={() => handlePresetTap(preset)}
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
          onPress={() => {
            if (advanced) Keyboard.dismiss();
            setAdvanced(!advanced);
          }}
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

        {advanced ? (
          <View style={styles.advancedSection}>
            <AppText variant="label" tone="secondary" style={styles.fieldLabel}>
              Working Directory
            </AppText>
            <View style={styles.inputRow}>
              <TextInput
                style={[styles.input, styles.inputFlex]}
                value={cwd}
                onChangeText={setCwd}
                placeholder="Leave empty for shell default"
                placeholderTextColor={colors.textSecondary}
                autoCapitalize="none"
                autoCorrect={false}
                autoComplete="off"
              />
              {selectedServerId ? (
                <IconButton
                  icon="folder-open-outline"
                  size={38}
                  iconSize={20}
                  tone="input"
                  style={styles.folderBtn}
                  onPress={() => {
                    Keyboard.dismiss();
                    setDirPickerOpen(true);
                  }}
                />
              ) : null}
            </View>

            <AppText variant="label" tone="secondary" style={styles.fieldLabel}>
              Command
            </AppText>
            <TextInput
              style={styles.input}
              value={command}
              onChangeText={setCommand}
              placeholder="e.g. claude --dangerously-skip-permissions"
              placeholderTextColor={colors.textSecondary}
              autoCapitalize="none"
              autoCorrect={false}
              autoComplete="off"
            />

            <AppText variant="label" tone="secondary" style={styles.fieldLabel}>
              Window Title
            </AppText>
            <TextInput
              style={styles.input}
              value={name}
              onChangeText={setName}
              placeholder="Optional"
              placeholderTextColor={colors.textSecondary}
              autoCapitalize="none"
              autoCorrect={false}
              autoComplete="off"
            />

            <AppButton
              label={submitting ? 'Starting...' : 'Launch'}
              variant="primary"
              onPress={handleAdvancedSubmit}
              disabled={!canSubmit}
              style={styles.launchBtn}
            />
          </View>
        ) : null}

        <AppButton
          label="Cancel"
          variant="ghost"
          onPress={handleClose}
          style={styles.cancelBtn}
        />
      </ScrollView>
    </BottomSheetFrame>
  );

  return (
    <>
      {sheetContent}

      {selectedServerId ? (
        <DirectoryPicker
          visible={dirPickerOpen}
          serverId={selectedServerId}
          initialPath={cwd.trim() || undefined}
          onSelect={(path) => {
            setCwd(path);
            setDirPickerOpen(false);
          }}
          onClose={() => setDirPickerOpen(false)}
        />
      ) : null}
    </>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
  sheetCard: {
    paddingHorizontal: 14,
    paddingTop: 10,
    paddingBottom: 18,
  },

  // Server chips
  serverSection: {
    marginBottom: 8,
  },
  serverRow: {
    flexDirection: 'row',
    gap: 8,
  },
  serverChip: {
    paddingHorizontal: 10,
    height: 28,
    borderRadius: 10,
    justifyContent: 'center',
    backgroundColor: colors.bgElevated,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.borderSubtle,
  },
  serverChipActive: {
    backgroundColor: colors.surfaceActive,
    borderColor: colors.accent,
  },

  // Preset cards
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

  // Advanced
  advancedToggle: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    marginTop: 8,
    paddingVertical: 6,
  },
  advancedSection: {
    marginTop: 4,
    gap: 4,
  },
  fieldLabel: {
    marginTop: 8,
    marginBottom: 4,
  },
  inputRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  inputFlex: {
    flex: 1,
  },
  input: {
    minHeight: 38,
    borderRadius: 10,
    paddingHorizontal: 12,
    color: colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.terminalFont,
    backgroundColor: colors.inputBackground,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.borderSubtle,
  },
  folderBtn: {
    alignSelf: 'stretch',
  },
  launchBtn: {
    minHeight: 36,
    borderRadius: 10,
    marginTop: 10,
  },

  // Cancel
  cancelBtn: {
    minHeight: 34,
    borderRadius: 10,
    marginTop: 8,
  },
  });
}
