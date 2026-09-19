import React from "react";
import {
  ScrollView,
  StyleSheet,
} from "react-native";
import { AppText } from "../ui";
import { NewTerminalAdvancedForm } from "./NewTerminalAdvancedForm";
import {
  NewTerminalQuickLaunchSection,
  type NewTerminalLaunchPreset,
} from "./NewTerminalQuickLaunchSection";

interface NewTerminalSheetContentProps {
  title: string;
  command: string;
  submitting: boolean;
  canSubmit: boolean;
  advanced: boolean;
  cwd: string;
  name: string;
  canPickDirectory: boolean;
  onPresetPress(preset: NewTerminalLaunchPreset): void;
  onToggleAdvanced(): void;
  onCwdChange(value: string): void;
  onCommandChange(value: string): void;
  onNameChange(value: string): void;
  onPickDirectory(): void;
  onSubmitAdvanced(): void;
}

export function NewTerminalSheetContent({
  title,
  command,
  submitting,
  canSubmit,
  advanced,
  cwd,
  name,
  canPickDirectory,
  onPresetPress,
  onToggleAdvanced,
  onCwdChange,
  onCommandChange,
  onNameChange,
  onPickDirectory,
  onSubmitAdvanced,
}: NewTerminalSheetContentProps) {
  return (
    <ScrollView
      keyboardShouldPersistTaps="handled"
      contentContainerStyle={styles.content}
      showsVerticalScrollIndicator={false}
    >
      <AppText variant="title" style={styles.title}>
        {title}
      </AppText>
      <NewTerminalQuickLaunchSection
        command={command}
        cwd={cwd}
        submitting={submitting}
        canSubmit={canSubmit}
        advanced={advanced}
        canPickDirectory={canPickDirectory}
        onPresetPress={onPresetPress}
        onToggleAdvanced={onToggleAdvanced}
        onPickDirectory={onPickDirectory}
      />
      {advanced ? (
        <NewTerminalAdvancedForm
          cwd={cwd}
          command={command}
          name={name}
          submitting={submitting}
          canSubmit={canSubmit}
          canPickDirectory={canPickDirectory}
          onCwdChange={onCwdChange}
          onCommandChange={onCommandChange}
          onNameChange={onNameChange}
          onPickDirectory={onPickDirectory}
          onSubmit={onSubmitAdvanced}
        />
      ) : null}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  content: {
    gap: 12,
    paddingBottom: 8,
  },
  title: {
    marginBottom: 4,
  },
});
