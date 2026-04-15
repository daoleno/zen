import React, { useEffect, useMemo, useState } from 'react';
import {
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
import { Colors, Typography } from '../../constants/tokens';
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
  description: string;
  command: string;
};

const PRESETS: readonly LaunchPreset[] = [
  { key: 'shell', kind: 'terminal', label: 'Shell', description: 'Plain terminal session', command: '' },
  { key: 'claude', kind: 'claude', label: 'Claude', description: 'Claude Code agent', command: CLAUDE_CODE_COMMAND },
  { key: 'codex', kind: 'codex', label: 'Codex', description: 'OpenAI Codex agent', command: CODEX_COMMAND },
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
  const [cwd, setCwd] = useState(initialCwd);
  const [command, setCommand] = useState(initialCommand);
  const [name, setName] = useState(initialName);
  const [advanced, setAdvanced] = useState(false);
  const [dirPickerOpen, setDirPickerOpen] = useState(false);

  useEffect(() => {
    if (!visible) return;
    setCwd(initialCwd);
    setCommand(initialCommand);
    setName(initialName);
    setAdvanced(false);
  }, [initialCommand, initialCwd, initialName, visible]);

  const canSubmit = !submitting && (!serverOptions.length || Boolean(selectedServerId));

  const activePreset = useMemo(
    () => PRESETS.find(p => p.command === command.trim())?.key ?? null,
    [command],
  );

  const handlePresetTap = (preset: LaunchPreset) => {
    if (!canSubmit) return;
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
    onSubmit({
      cwd: cwd.trim(),
      command: command.trim(),
      name: name.trim(),
      serverId: selectedServerId ?? undefined,
    });
  };

  return (
    <Modal
      visible={visible}
      transparent
      animationType="fade"
      onRequestClose={onClose}
    >
      <KeyboardAvoidingView
        style={styles.root}
        behavior={Platform.OS === 'ios' ? 'padding' : 'height'}
      >
        <TouchableOpacity
          style={styles.backdrop}
          activeOpacity={1}
          onPress={onClose}
        />
        <View style={styles.card}>
          <View style={styles.handle} />

          <ScrollView
            showsVerticalScrollIndicator={false}
            keyboardShouldPersistTaps="handled"
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
                    <Text style={styles.presetDesc}>{preset.description}</Text>
                  </View>
                  <Ionicons name="chevron-forward" size={16} color="rgba(255,255,255,0.2)" />
                </TouchableOpacity>
              ))}
            </View>

            {/* Advanced toggle */}
            <TouchableOpacity
              style={styles.advancedToggle}
              onPress={() => setAdvanced(!advanced)}
              activeOpacity={0.82}
            >
              <Ionicons
                name={advanced ? 'chevron-down' : 'chevron-forward'}
                size={14}
                color={Colors.textSecondary}
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
                    placeholderTextColor="#6E7D90"
                    autoCapitalize="none"
                    autoCorrect={false}
                    autoComplete="off"
                  />
                  {selectedServerId ? (
                    <TouchableOpacity
                      style={styles.folderBtn}
                      onPress={() => setDirPickerOpen(true)}
                      activeOpacity={0.7}
                    >
                      <Ionicons name="folder-open-outline" size={20} color={Colors.textSecondary} />
                    </TouchableOpacity>
                  ) : null}
                </View>

                <Text style={styles.fieldLabel}>Command</Text>
                <TextInput
                  style={styles.input}
                  value={command}
                  onChangeText={setCommand}
                  placeholder="e.g. claude --dangerously-skip-permissions"
                  placeholderTextColor="#6E7D90"
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
                  placeholderTextColor="#6E7D90"
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
              onPress={onClose}
              activeOpacity={0.82}
            >
              <Text style={styles.cancelBtnText}>Cancel</Text>
            </TouchableOpacity>
          </ScrollView>
        </View>
      </KeyboardAvoidingView>

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

const styles = StyleSheet.create({
  root: {
    flex: 1,
    justifyContent: 'flex-end',
  },
  backdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: 'rgba(6, 8, 12, 0.62)',
  },
  card: {
    maxHeight: '85%',
    borderTopLeftRadius: 28,
    borderTopRightRadius: 28,
    paddingHorizontal: 18,
    paddingTop: 12,
    paddingBottom: 28,
    backgroundColor: '#121A25',
    borderTopWidth: 1,
    borderColor: 'rgba(255,255,255,0.06)',
  },
  handle: {
    alignSelf: 'center',
    width: 42,
    height: 4,
    borderRadius: 2,
    backgroundColor: '#3A475B',
    marginBottom: 18,
  },

  // Server chips
  serverSection: {
    marginBottom: 14,
  },
  serverRow: {
    flexDirection: 'row',
    gap: 8,
  },
  serverChip: {
    paddingHorizontal: 12,
    height: 34,
    borderRadius: 12,
    justifyContent: 'center',
    backgroundColor: '#18222F',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.06)',
  },
  serverChipActive: {
    backgroundColor: 'rgba(214, 177, 106, 0.14)',
    borderColor: 'rgba(214, 177, 106, 0.42)',
  },
  serverChipText: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  serverChipTextActive: {
    color: Colors.textPrimary,
  },

  // Preset cards
  presetList: {
    gap: 8,
  },
  presetCard: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 14,
    paddingVertical: 14,
    paddingHorizontal: 16,
    borderRadius: 14,
    backgroundColor: '#18222F',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.06)',
  },
  presetCardActive: {
    borderColor: 'rgba(91,157,255,0.38)',
    backgroundColor: 'rgba(91,157,255,0.08)',
  },
  presetCardDisabled: {
    opacity: 0.5,
  },
  presetCardText: {
    flex: 1,
  },
  presetLabel: {
    color: Colors.textPrimary,
    fontSize: 15,
    fontFamily: Typography.uiFontMedium,
  },
  presetDesc: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
    marginTop: 2,
    opacity: 0.7,
  },

  // Advanced
  advancedToggle: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    marginTop: 16,
    paddingVertical: 8,
  },
  advancedToggleText: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFont,
  },
  advancedSection: {
    marginTop: 4,
    gap: 4,
  },
  fieldLabel: {
    color: Colors.textSecondary,
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
    minHeight: 42,
    borderRadius: 12,
    paddingHorizontal: 14,
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.terminalFont,
    backgroundColor: '#18222F',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.06)',
  },
  folderBtn: {
    width: 42,
    minHeight: 42,
    borderRadius: 12,
    backgroundColor: '#18222F',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.06)',
    alignItems: 'center',
    justifyContent: 'center',
    alignSelf: 'stretch',
  },
  launchBtn: {
    height: 40,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: Colors.accent,
    marginTop: 12,
  },
  launchBtnDisabled: {
    opacity: 0.5,
  },
  launchBtnText: {
    color: Colors.bgPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },

  // Cancel
  cancelBtn: {
    height: 40,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 12,
    backgroundColor: 'rgba(255,255,255,0.04)',
  },
  cancelBtnText: {
    color: Colors.textSecondary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
});
