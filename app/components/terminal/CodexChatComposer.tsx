import React from "react";
import {
  StyleSheet,
  View,
  type LayoutChangeEvent,
  type TextInput as TextInputInstance,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { CodexSlashCommand } from "../../services/websocket";
import {
  CodexComposerAttachmentRail,
  type CodexComposerAttachment,
} from "./CodexComposerAttachmentRail";
import { CodexComposerPanel } from "./CodexComposerPanel";
import { CodexQuickCommandMenu } from "./CodexQuickCommandMenu";

interface CodexChatComposerProps {
  inputRef: React.RefObject<TextInputInstance | null>;
  draft: string;
  placeholder: string;
  editable: boolean;
  focused: boolean;
  floating: boolean;
  canAttach: boolean;
  uploading: boolean;
  sendEnabled: boolean;
  sending: boolean;
  sendIcon: React.ComponentProps<typeof CodexComposerPanel>["sendIcon"];
  sendLabel: string;
  compactSendIcon: boolean;
  bottomPadding: number;
  showCommandMenu: boolean;
  commandQuery: string;
  commands: CodexSlashCommand[];
  attachments: CodexComposerAttachment[];
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onLayout(event: LayoutChangeEvent): void;
  onSelectCommand(command: CodexSlashCommand): void;
  onRemoveAttachment(id: string): void;
  onDraftChange(value: string): void;
  onUploadPress(): void;
  onInputPress(): void;
  onInputFocus(): void;
  onInputBlur(): void;
  onInputStart(): boolean;
  onSubmit(): void;
  onSendPress(): void;
}

export function CodexChatComposer({
  inputRef,
  draft,
  placeholder,
  editable,
  focused,
  floating,
  canAttach,
  uploading,
  sendEnabled,
  sending,
  sendIcon,
  sendLabel,
  compactSendIcon,
  bottomPadding,
  showCommandMenu,
  commandQuery,
  commands,
  attachments,
  chrome,
  theme,
  onLayout,
  onSelectCommand,
  onRemoveAttachment,
  onDraftChange,
  onUploadPress,
  onInputPress,
  onInputFocus,
  onInputBlur,
  onInputStart,
  onSubmit,
  onSendPress,
}: CodexChatComposerProps) {
  return (
    <View
      onLayout={onLayout}
      style={[
        styles.composer,
        {
          paddingBottom: bottomPadding,
          borderTopColor: chrome.border,
          backgroundColor: theme.background,
        },
      ]}
    >
      {showCommandMenu ? (
        <CodexQuickCommandMenu
          commands={commands}
          commandQuery={commandQuery}
          chrome={chrome}
          theme={theme}
          onSelectCommand={onSelectCommand}
        />
      ) : null}

      <CodexComposerAttachmentRail
        attachments={attachments}
        uploading={uploading}
        chrome={chrome}
        onRemoveAttachment={onRemoveAttachment}
      />

      <CodexComposerPanel
        inputRef={inputRef}
        draft={draft}
        placeholder={placeholder}
        editable={editable}
        focused={focused}
        floating={floating}
        canAttach={canAttach}
        uploading={uploading}
        sendEnabled={sendEnabled}
        sending={sending}
        sendIcon={sendIcon}
        sendLabel={sendLabel}
        compactSendIcon={compactSendIcon}
        chrome={chrome}
        theme={theme}
        onDraftChange={onDraftChange}
        onUploadPress={onUploadPress}
        onInputPress={onInputPress}
        onInputFocus={onInputFocus}
        onInputBlur={onInputBlur}
        onInputStart={onInputStart}
        onSubmit={onSubmit}
        onSendPress={onSendPress}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  composer: {
    paddingHorizontal: 12,
    paddingTop: 8,
  },
});
