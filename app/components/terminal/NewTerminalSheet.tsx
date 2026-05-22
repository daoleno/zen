import React, { useEffect, useState } from 'react';
import {
  Keyboard,
  ScrollView,
  StyleSheet,
} from 'react-native';
import { DirectoryPicker } from './DirectoryPicker';
import { NewTerminalAdvancedForm } from './NewTerminalAdvancedForm';
import {
  NewTerminalQuickLaunchSection,
  type NewTerminalLaunchPreset,
  type NewTerminalServerOption,
} from './NewTerminalQuickLaunchSection';
import { AppButton, BottomSheetFrame } from '../ui';

interface NewTerminalSheetProps {
  visible: boolean;
  title: string;
  subtitle: string;
  initialCwd?: string;
  initialCommand?: string;
  initialName?: string;
  submitting?: boolean;
  serverOptions?: NewTerminalServerOption[];
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
    Keyboard.dismiss();
    setCwd(initialCwd);
    setCommand(initialCommand);
    setName(initialName);
    setAdvanced(false);
    setDirPickerOpen(false);
  }, [initialCommand, initialCwd, initialName, visible]);

  const canSubmit = !submitting && (!serverOptions.length || Boolean(selectedServerId));

  const handleClose = () => {
    Keyboard.dismiss();
    onClose();
  };

  const handlePresetTap = (preset: NewTerminalLaunchPreset) => {
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

  const handleOpenDirectoryPicker = () => {
    Keyboard.dismiss();
    setDirPickerOpen(true);
  };

  const handleToggleAdvanced = () => {
    if (advanced) Keyboard.dismiss();
    setAdvanced(!advanced);
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
        <NewTerminalQuickLaunchSection
          serverOptions={serverOptions}
          selectedServerId={selectedServerId}
          command={command}
          submitting={submitting}
          canSubmit={canSubmit}
          advanced={advanced}
          onSelectServer={onSelectServer}
          onPresetPress={handlePresetTap}
          onToggleAdvanced={handleToggleAdvanced}
        />

        {advanced ? (
          <NewTerminalAdvancedForm
            cwd={cwd}
            command={command}
            name={name}
            canSubmit={canSubmit}
            submitting={submitting}
            canPickDirectory={Boolean(selectedServerId)}
            onCwdChange={setCwd}
            onCommandChange={setCommand}
            onNameChange={setName}
            onPickDirectory={handleOpenDirectoryPicker}
            onSubmit={handleAdvancedSubmit}
          />
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

const styles = StyleSheet.create({
  sheetCard: {
    paddingHorizontal: 14,
    paddingTop: 10,
    paddingBottom: 18,
  },
  cancelBtn: {
    minHeight: 34,
    borderRadius: 10,
    marginTop: 8,
  },
});
