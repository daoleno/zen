import React, { useMemo } from 'react';
import {
  StyleSheet,
  TextInput,
  View,
} from 'react-native';
import { Colors, Typography, useAppColors } from '../../constants/tokens';
import { AppButton, AppText, IconButton } from '../ui';

interface NewTerminalAdvancedFormProps {
  cwd: string;
  command: string;
  name: string;
  canSubmit: boolean;
  submitting: boolean;
  canPickDirectory: boolean;
  onCwdChange(value: string): void;
  onCommandChange(value: string): void;
  onNameChange(value: string): void;
  onPickDirectory(): void;
  onSubmit(): void;
}

export function NewTerminalAdvancedForm({
  cwd,
  command,
  name,
  canSubmit,
  submitting,
  canPickDirectory,
  onCwdChange,
  onCommandChange,
  onNameChange,
  onPickDirectory,
  onSubmit,
}: NewTerminalAdvancedFormProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);

  return (
    <View style={styles.advancedSection}>
      <AppText variant="label" tone="secondary" style={styles.fieldLabel}>
        Working Directory
      </AppText>
      <View style={styles.inputRow}>
        <TextInput
          style={[styles.input, styles.inputFlex]}
          value={cwd}
          onChangeText={onCwdChange}
          placeholder="Leave empty for shell default"
          placeholderTextColor={colors.textSecondary}
          autoCapitalize="none"
          autoCorrect={false}
          autoComplete="off"
        />
        {canPickDirectory ? (
          <IconButton
            icon="folder-open-outline"
            size={38}
            iconSize={20}
            tone="input"
            style={styles.folderBtn}
            onPress={onPickDirectory}
          />
        ) : null}
      </View>

      <AppText variant="label" tone="secondary" style={styles.fieldLabel}>
        Command
      </AppText>
      <TextInput
        style={styles.input}
        value={command}
        onChangeText={onCommandChange}
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
        onChangeText={onNameChange}
        placeholder="Optional"
        placeholderTextColor={colors.textSecondary}
        autoCapitalize="none"
        autoCorrect={false}
        autoComplete="off"
      />

      <AppButton
        label={submitting ? 'Starting...' : 'Launch'}
        variant="primary"
        onPress={onSubmit}
        disabled={!canSubmit}
        style={styles.launchBtn}
      />
    </View>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
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
  });
}
