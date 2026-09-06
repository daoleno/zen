import React, { useCallback, useEffect, useRef, useState } from "react";
import { Keyboard, StyleSheet } from "react-native";
import { wsClient } from "../../services/websocket";
import { useCurrentServer } from "../../store/currentServer";
import { DirectoryPickerContent } from "./DirectoryPickerContent";
import {
  beginDirectoryLoad,
  completeDirectoryLoad,
  createIdleDirectoryPickerState,
  failDirectoryLoad,
  nextDirectoryListEpoch,
  parentDirectoryPath,
  shouldApplyDirectoryListResult,
  type DirectoryPickerViewState,
} from "./directoryPickerState";
import {
  type NewTerminalLaunchPreset,
} from "./NewTerminalQuickLaunchSection";
import { NewTerminalSheetContent } from "./NewTerminalSheetContent";
import {
  createNewTerminalSheetFormState,
  openDirectoryPanel,
  resolveNewTerminalSheetDismiss,
  returnToFormPanel,
  selectDirectoryPath,
  type NewTerminalSheetFormState,
} from "./newTerminalSheetModel";
import { BottomSheetFrame } from "../ui";

interface NewTerminalSheetProps {
  visible: boolean;
  title: string;
  initialCwd?: string;
  initialCommand?: string;
  initialName?: string;
  submitting?: boolean;
  serverId: string | null;
  onClose(): void;
  onSubmit(input: {
    cwd: string;
    command: string;
    name: string;
    serverId?: string;
  }): void;
}

export function NewTerminalSheet({
  visible,
  title,
  initialCwd = "",
  initialCommand = "",
  initialName = "",
  submitting = false,
  serverId,
  onClose,
  onSubmit,
}: NewTerminalSheetProps) {
  const { isCurrentServer } = useCurrentServer();
  const [form, setForm] = useState<NewTerminalSheetFormState>(() =>
    createNewTerminalSheetFormState({
      cwd: initialCwd,
      command: initialCommand,
      name: initialName,
    }),
  );
  const [directory, setDirectory] = useState<DirectoryPickerViewState>(
    createIdleDirectoryPickerState,
  );
  const listDirEpochRef = useRef(0);

  const invalidateDirectoryRequests = useCallback(() => {
    listDirEpochRef.current = nextDirectoryListEpoch(listDirEpochRef.current);
  }, []);

  useEffect(() => {
    return () => {
      invalidateDirectoryRequests();
    };
  }, [invalidateDirectoryRequests]);

  useEffect(() => {
    if (!visible) {
      invalidateDirectoryRequests();
      return;
    }
    setForm(
      createNewTerminalSheetFormState({
        cwd: initialCwd,
        command: initialCommand,
        name: initialName,
      }),
    );
    setDirectory(createIdleDirectoryPickerState());
  }, [
    initialCommand,
    initialCwd,
    initialName,
    invalidateDirectoryRequests,
    serverId,
    visible,
  ]);

  const canSubmit = !submitting && isCurrentServer(serverId);

  const loadDirectory = useCallback(async (serverId: string, path?: string) => {
    if (!isCurrentServer(serverId)) return;
    const epoch = nextDirectoryListEpoch(listDirEpochRef.current);
    listDirEpochRef.current = epoch;
    setDirectory((current) => beginDirectoryLoad(current));
    try {
      const result = await wsClient.listDir(serverId, path);
      if (!isCurrentServer(serverId) || !shouldApplyDirectoryListResult(epoch, listDirEpochRef.current)) {
        return;
      }
      setDirectory((current) => completeDirectoryLoad(current, result));
    } catch (e: any) {
      if (!isCurrentServer(serverId) || !shouldApplyDirectoryListResult(epoch, listDirEpochRef.current)) {
        return;
      }
      setDirectory((current) =>
        failDirectoryLoad(
          current,
          e instanceof Error ? e.message : "Failed to list directory",
        ),
      );
    }
  }, [isCurrentServer]);

  const submitLaunch = (input: {
    cwd: string;
    command: string;
    name: string;
  }) => {
    if (!serverId || !isCurrentServer(serverId)) return;
    onSubmit({
      ...input,
      serverId,
    });
  };

  const handlePresetTap = (preset: NewTerminalLaunchPreset) => {
    if (!canSubmit) return;
    Keyboard.dismiss();
    setForm((current) => ({ ...current, command: preset.command }));
    if (form.advanced) {
      return;
    }
    submitLaunch({
      cwd: form.cwd.trim() || initialCwd.trim(),
      command: preset.command,
      name: "",
    });
  };

  const handleAdvancedSubmit = () => {
    if (!canSubmit) return;
    Keyboard.dismiss();
    submitLaunch({
      cwd: form.cwd.trim(),
      command: form.command.trim(),
      name: form.name.trim(),
    });
  };

  const handleOpenDirectoryPicker = () => {
    if (!serverId) return;
    Keyboard.dismiss();
    setDirectory(createIdleDirectoryPickerState());
    setForm((current) => openDirectoryPanel(current));
    void loadDirectory(serverId, form.cwd.trim() || undefined);
  };

  const handleReturnToForm = () => {
    invalidateDirectoryRequests();
    setForm((current) => returnToFormPanel(current));
  };

  const handleSheetDismiss = () => {
    const next = resolveNewTerminalSheetDismiss(form.panel);
    if (next === "close-sheet") {
      onClose();
      return;
    }
    handleReturnToForm();
  };

  const handleCloseSheet = () => {
    onClose();
  };

  const handleSelectDirectory = () => {
    if (!directory.currentPath) return;
    invalidateDirectoryRequests();
    setForm((current) => selectDirectoryPath(current, directory.currentPath));
  };

  const handleToggleAdvanced = () => {
    if (form.advanced) Keyboard.dismiss();
    setForm((current) => ({ ...current, advanced: !current.advanced }));
  };

  return (
    <BottomSheetFrame
      visible={visible}
      onClose={handleSheetDismiss}
      maxHeight="68%"
      cardStyle={styles.sheetCard}
      keyboardAvoiding
    >
      {form.panel === "directory" ? (
        <DirectoryPickerContent
          currentPath={directory.currentPath}
          entries={directory.entries}
          loading={directory.loading}
          error={directory.error}
          onGoUp={() => {
            if (!serverId) return;
            void loadDirectory(
              serverId,
              parentDirectoryPath(directory.currentPath),
            );
          }}
          onOpenDirectory={(path) => {
            if (!serverId) return;
            void loadDirectory(serverId, path);
          }}
          onSelectCurrent={handleSelectDirectory}
          onClose={handleReturnToForm}
        />
      ) : (
        <NewTerminalSheetContent
          title={title}
          command={form.command}
          submitting={submitting}
          canSubmit={canSubmit}
          advanced={form.advanced}
          cwd={form.cwd}
          name={form.name}
          canPickDirectory={Boolean(serverId)}
          onPresetPress={handlePresetTap}
          onToggleAdvanced={handleToggleAdvanced}
          onCwdChange={(cwd) => setForm((current) => ({ ...current, cwd }))}
          onCommandChange={(command) =>
            setForm((current) => ({ ...current, command }))
          }
          onNameChange={(name) => setForm((current) => ({ ...current, name }))}
          onPickDirectory={handleOpenDirectoryPicker}
          onSubmitAdvanced={handleAdvancedSubmit}
        />
      )}
    </BottomSheetFrame>
  );
}

const styles = StyleSheet.create({
  sheetCard: {
    paddingHorizontal: 14,
    paddingTop: 10,
    paddingBottom: 18,
  },
});
