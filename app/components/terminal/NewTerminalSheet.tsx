import React, { useEffect, useState } from 'react';
import {
  Keyboard,
  StyleSheet,
} from 'react-native';
import { DirectoryPicker } from './DirectoryPicker';
import {
  type NewTerminalLaunchPreset,
  type NewTerminalServerOption,
} from './NewTerminalQuickLaunchSection';
import { NewTerminalSheetContent } from './NewTerminalSheetContent';
import { BottomSheetFrame } from '../ui';

interface NewTerminalSheetProps {
  visible: boolean;
  title: string;
  subtitle: string;
  initialCwd?: string;
  initialCommand?: string;
  initialName?: string;
  attentionWarning?: string;
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
  attentionWarning,
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
      <NewTerminalSheetContent
        serverOptions={serverOptions}
        selectedServerId={selectedServerId}
        command={command}
        submitting={submitting}
        attentionWarning={attentionWarning}
        canSubmit={canSubmit}
        advanced={advanced}
        cwd={cwd}
        name={name}
        canPickDirectory={Boolean(selectedServerId)}
        onSelectServer={onSelectServer}
        onPresetPress={handlePresetTap}
        onToggleAdvanced={handleToggleAdvanced}
        onCwdChange={setCwd}
        onCommandChange={setCommand}
        onNameChange={setName}
        onPickDirectory={handleOpenDirectoryPicker}
        onSubmitAdvanced={handleAdvancedSubmit}
        onCancel={handleClose}
      />
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
});
