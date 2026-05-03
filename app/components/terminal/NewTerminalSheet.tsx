import React, { useEffect, useMemo, useState } from 'react';
import {
  Keyboard,
  KeyboardAvoidingView,
  Modal,
  Platform,
  ScrollView,
  StyleSheet,
  Text,
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
    <>
        <TouchableOpacity
          style={styles.backdrop}
          activeOpacity={1}
          onPress={handleClose}
        />
        <View style={styles.card}>
          <View style={styles.handle} />

          <ScrollView
            showsVerticalScrollIndicator={false}
            keyboardShouldPersistTaps="always"
            keyboardDismissMode="none"
            bounces={false}
          >
            {/* Server selector when multiple servers */}
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
                          <Text style={[styles.serverChipText, active && styles.serverChipTextActive]}>
                            {server.name}
                          </Text>
                        </TouchableOpacity>
                      );
                    })}
                  </View>
                </ScrollView>
              </View>
            ) : null}

            {/* Preset cards */}
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
                    <Text style={styles.presetLabel}>{preset.label}</Text>
                  </View>
                  <Ionicons name="chevron-forward" size={14} color={colors.textSecondary} />
                </TouchableOpacity>
              ))}
            </View>

            {/* Advanced toggle */}
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
              <Text style={styles.advancedToggleText}>Advanced</Text>
            </TouchableOpacity>

            {/* Advanced fields */}
            {advanced ? (
              <View style={styles.advancedSection}>
                <Text style={styles.fieldLabel}>Working Directory</Text>
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
                    <TouchableOpacity
                      style={styles.folderBtn}
                      onPress={() => {
                        Keyboard.dismiss();
                        setDirPickerOpen(true);
                      }}
                      activeOpacity={0.7}
                    >
                      <Ionicons name="folder-open-outline" size={20} color={colors.textSecondary} />
                    </TouchableOpacity>
                  ) : null}
                </View>

                <Text style={styles.fieldLabel}>Command</Text>
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

                <Text style={styles.fieldLabel}>Window Title</Text>
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

                <TouchableOpacity
                  style={[styles.launchBtn, !canSubmit && styles.launchBtnDisabled]}
                  onPress={handleAdvancedSubmit}
                  disabled={!canSubmit}
                  activeOpacity={0.82}
                >
                  <Text style={styles.launchBtnText}>
                    {submitting ? 'Starting…' : 'Launch'}
                  </Text>
                </TouchableOpacity>
              </View>
            ) : null}

            {/* Cancel */}
            <TouchableOpacity
              style={styles.cancelBtn}
              onPress={handleClose}
              activeOpacity={0.82}
            >
              <Text style={styles.cancelBtnText}>Cancel</Text>
            </TouchableOpacity>
          </ScrollView>
        </View>
    </>
  );

  return (
    <Modal
      visible={visible}
      transparent
      animationType="fade"
      onRequestClose={handleClose}
    >
      {Platform.OS === 'ios' ? (
        <KeyboardAvoidingView style={styles.root} behavior="padding">
          {sheetContent}
        </KeyboardAvoidingView>
      ) : (
        <View style={styles.root}>{sheetContent}</View>
      )}

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
    </Modal>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
  root: {
    flex: 1,
    justifyContent: 'flex-end',
  },
  backdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: colors.modalBackdrop,
  },
  card: {
    maxHeight: '68%',
    borderTopLeftRadius: 28,
    borderTopRightRadius: 28,
    paddingHorizontal: 14,
    paddingTop: 10,
    paddingBottom: 18,
    backgroundColor: colors.modalSurface,
    borderTopWidth: 1,
    borderColor: colors.borderSubtle,
  },
  handle: {
    alignSelf: 'center',
    width: 42,
    height: 3,
    borderRadius: 2,
    backgroundColor: colors.borderStrong,
    marginBottom: 10,
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
  serverChipText: {
    color: colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  serverChipTextActive: {
    color: colors.textPrimary,
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
  presetLabel: {
    color: colors.textPrimary,
    fontSize: 14,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
  },

  // Advanced
  advancedToggle: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    marginTop: 8,
    paddingVertical: 6,
  },
  advancedToggleText: {
    color: colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  advancedSection: {
    marginTop: 4,
    gap: 4,
  },
  fieldLabel: {
    color: colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
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
    width: 38,
    minHeight: 38,
    borderRadius: 10,
    backgroundColor: colors.inputBackground,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.borderSubtle,
    alignItems: 'center',
    justifyContent: 'center',
    alignSelf: 'stretch',
  },
  launchBtn: {
    height: 36,
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.accent,
    marginTop: 10,
  },
  launchBtnDisabled: {
    opacity: 0.5,
  },
  launchBtnText: {
    color: colors.textOnAccent,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },

  // Cancel
  cancelBtn: {
    height: 34,
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 8,
    backgroundColor: 'transparent',
  },
  cancelBtnText: {
    color: colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  });
}
