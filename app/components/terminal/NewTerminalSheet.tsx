import React, { useEffect, useMemo, useState } from 'react';
import {
  Modal,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { Colors, Typography } from '../../constants/tokens';

type ServerOption = {
  id: string;
  name: string;
};

type LaunchPreset = {
  key: string;
  label: string;
  command: string;
};

const PRESETS: readonly LaunchPreset[] = [
  { key: 'shell', label: 'Shell', command: '' },
  { key: 'claude', label: 'Claude', command: 'claude' },
  { key: 'codex', label: 'Codex', command: 'codex' },
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
  title,
  subtitle,
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

  useEffect(() => {
    if (!visible) return;
    setCwd(initialCwd);
    setCommand(initialCommand);
    setName(initialName);
  }, [initialCommand, initialCwd, initialName, visible]);

  const activePreset = useMemo(
    () => PRESETS.find(preset => preset.command === command.trim())?.key ?? null,
    [command],
  );

  const submitLabel = command.trim() ? 'Launch Terminal' : 'Create Terminal';
  const canSubmit = !submitting && (!serverOptions.length || Boolean(selectedServerId));

  return (
    <Modal
      visible={visible}
      transparent
      animationType="fade"
      onRequestClose={onClose}
    >
      <View style={styles.root}>
        <TouchableOpacity
          style={styles.backdrop}
          activeOpacity={1}
          onPress={onClose}
        />
        <View style={styles.card}>
          <View style={styles.handle} />
          <Text style={styles.title}>{title}</Text>
          <Text style={styles.subtitle}>{subtitle}</Text>

          {serverOptions.length > 1 ? (
            <View style={styles.section}>
              <Text style={styles.sectionLabel}>Server</Text>
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

          <View style={styles.section}>
            <Text style={styles.sectionLabel}>Launch</Text>
            <View style={styles.presetRow}>
              {PRESETS.map(preset => {
                const active = preset.key === activePreset;
                return (
                  <TouchableOpacity
                    key={preset.key}
                    style={[styles.presetChip, active && styles.presetChipActive]}
                    onPress={() => setCommand(preset.command)}
                    activeOpacity={0.84}
                  >
                    <Text style={[styles.presetChipText, active && styles.presetChipTextActive]}>
                      {preset.label}
                    </Text>
                  </TouchableOpacity>
                );
              })}
            </View>
          </View>

          <View style={styles.section}>
            <Text style={styles.sectionLabel}>Working Directory</Text>
            <TextInput
              style={styles.input}
              value={cwd}
              onChangeText={setCwd}
              placeholder="Optional. Leave empty to use the shell default."
              placeholderTextColor="#6E7D90"
              autoCapitalize="none"
              autoCorrect={false}
              autoComplete="off"
            />
          </View>

          <View style={styles.section}>
            <Text style={styles.sectionLabel}>Command</Text>
            <TextInput
              style={styles.input}
              value={command}
              onChangeText={setCommand}
              placeholder="Optional. Example: claude or codex --approval-mode auto"
              placeholderTextColor="#6E7D90"
              autoCapitalize="none"
              autoCorrect={false}
              autoComplete="off"
            />
          </View>

          <View style={styles.section}>
            <Text style={styles.sectionLabel}>Window Title</Text>
            <TextInput
              style={styles.input}
              value={name}
              onChangeText={setName}
              placeholder="Optional. Example: claude-api"
              placeholderTextColor="#6E7D90"
              autoCapitalize="none"
              autoCorrect={false}
              autoComplete="off"
            />
          </View>

          <Text style={styles.note}>
            Leave the command empty to open a plain shell. If a directory is set, tmux starts the new terminal there.
          </Text>

          <View style={styles.actions}>
            <TouchableOpacity
              style={styles.secondaryButton}
              onPress={onClose}
              activeOpacity={0.84}
            >
              <Text style={styles.secondaryButtonText}>Cancel</Text>
            </TouchableOpacity>
            <TouchableOpacity
              style={[styles.primaryButton, !canSubmit && styles.primaryButtonDisabled]}
              onPress={() => onSubmit({
                cwd: cwd.trim(),
                command: command.trim(),
                name: name.trim(),
                serverId: selectedServerId ?? undefined,
              })}
              disabled={!canSubmit}
              activeOpacity={0.84}
            >
              <Ionicons name={command.trim() ? 'rocket-outline' : 'add'} size={16} color={Colors.bgPrimary} />
              <Text style={styles.primaryButtonText}>
                {submitting ? 'Starting…' : submitLabel}
              </Text>
            </TouchableOpacity>
          </View>
        </View>
      </View>
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
    marginBottom: 14,
  },
  title: {
    color: Colors.textPrimary,
    fontSize: 20,
    fontFamily: Typography.uiFontMedium,
  },
  subtitle: {
    marginTop: 6,
    color: '#7D8CA0',
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  section: {
    marginTop: 16,
  },
  sectionLabel: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
    marginBottom: 8,
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
  presetRow: {
    flexDirection: 'row',
    gap: 8,
  },
  presetChip: {
    paddingHorizontal: 12,
    height: 34,
    borderRadius: 12,
    justifyContent: 'center',
    backgroundColor: '#18222F',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.06)',
  },
  presetChipActive: {
    backgroundColor: 'rgba(214, 177, 106, 0.14)',
    borderColor: 'rgba(214, 177, 106, 0.42)',
  },
  presetChipText: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  presetChipTextActive: {
    color: Colors.textPrimary,
  },
  input: {
    minHeight: 44,
    borderRadius: 14,
    paddingHorizontal: 14,
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.terminalFont,
    backgroundColor: '#18222F',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.06)',
  },
  note: {
    marginTop: 14,
    color: '#7D8CA0',
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.uiFont,
  },
  actions: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
    gap: 10,
    marginTop: 20,
  },
  secondaryButton: {
    minWidth: 84,
    height: 40,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: '#18222F',
  },
  secondaryButtonText: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  primaryButton: {
    minWidth: 150,
    height: 40,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: Colors.accent,
    flexDirection: 'row',
    gap: 8,
    paddingHorizontal: 14,
  },
  primaryButtonDisabled: {
    opacity: 0.6,
  },
  primaryButtonText: {
    color: Colors.bgPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
});
