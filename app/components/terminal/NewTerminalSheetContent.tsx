import React from "react";
import {
  ScrollView,
  StyleSheet,
  View,
} from "react-native";
import { AppButton, AppText } from "../ui";
import { NewTerminalAdvancedForm } from "./NewTerminalAdvancedForm";
import {
  NewTerminalQuickLaunchSection,
  type NewTerminalLaunchPreset,
  type NewTerminalServerOption,
} from "./NewTerminalQuickLaunchSection";

interface NewTerminalSheetContentProps {
  serverOptions: NewTerminalServerOption[];
  selectedServerId?: string | null;
  command: string;
  submitting: boolean;
  attentionWarning?: string;
  canSubmit: boolean;
  advanced: boolean;
  cwd: string;
  name: string;
  canPickDirectory: boolean;
  onSelectServer?(serverId: string): void;
  onPresetPress(preset: NewTerminalLaunchPreset): void;
  onToggleAdvanced(): void;
  onCwdChange(value: string): void;
  onCommandChange(value: string): void;
  onNameChange(value: string): void;
  onPickDirectory(): void;
  onSubmitAdvanced(): void;
  onCancel(): void;
}

export function NewTerminalSheetContent({
  serverOptions,
  selectedServerId,
  command,
  submitting,
  attentionWarning,
  canSubmit,
  advanced,
  cwd,
  name,
  canPickDirectory,
  onSelectServer,
  onPresetPress,
  onToggleAdvanced,
  onCwdChange,
  onCommandChange,
  onNameChange,
  onPickDirectory,
  onSubmitAdvanced,
  onCancel,
}: NewTerminalSheetContentProps) {
  return (
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
        onPresetPress={onPresetPress}
        onToggleAdvanced={onToggleAdvanced}
      />

      {attentionWarning ? (
        <View style={styles.attentionWarning}>
          <AppText variant="caption" tone="secondary">
            {attentionWarning}
          </AppText>
        </View>
      ) : null}

      {advanced ? (
        <NewTerminalAdvancedForm
          cwd={cwd}
          command={command}
          name={name}
          canSubmit={canSubmit}
          submitting={submitting}
          canPickDirectory={canPickDirectory}
          onCwdChange={onCwdChange}
          onCommandChange={onCommandChange}
          onNameChange={onNameChange}
          onPickDirectory={onPickDirectory}
          onSubmit={onSubmitAdvanced}
        />
      ) : null}

      <AppButton
        label="Cancel"
        variant="ghost"
        onPress={onCancel}
        style={styles.cancelBtn}
      />
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  attentionWarning: {
    borderRadius: 8,
    marginTop: 10,
    paddingHorizontal: 10,
    paddingVertical: 8,
    backgroundColor: "rgba(245, 158, 11, 0.12)",
  },
  cancelBtn: {
    minHeight: 34,
    borderRadius: 10,
    marginTop: 8,
  },
});
