import React from "react";
import {
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
import { CodexComposerActionMenu } from "./CodexComposerActionMenu";
import { CodexChatComposerFrame } from "./CodexChatComposerFrame";
import { CodexComposerPanel } from "./CodexComposerPanel";

interface CodexChatComposerProps {
  inputRef: React.RefObject<TextInputInstance | null>;
  draft: string;
  placeholder: string;
  editable: boolean;
  focused: boolean;
  canAttach: boolean;
  uploading: boolean;
  sendEnabled: boolean;
  sending: boolean;
  sendIcon: React.ComponentProps<typeof CodexComposerPanel>["sendIcon"];
  sendLabel: string;
  sendElapsedLabel?: string;
  running: boolean;
  bottomPadding: number;
  showCommandMenu: boolean;
  showCommandList: boolean;
  showComposerActions: boolean;
  composerActionButtonEnabled: boolean;
  commandQuery: string;
  commands: CodexSlashCommand[];
  attachments: CodexComposerAttachment[];
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onLayout(event: LayoutChangeEvent): void;
  onSelectCommand(command: CodexSlashCommand): void;
  onToggleActionMenu(): void;
  onDismissActionMenu(): void;
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
  canAttach,
  uploading,
  sendEnabled,
  sending,
  sendIcon,
  sendLabel,
  sendElapsedLabel,
  running,
  bottomPadding,
  showCommandMenu,
  showCommandList,
  showComposerActions,
  composerActionButtonEnabled,
  commandQuery,
  commands,
  attachments,
  chrome,
  theme,
  onLayout,
  onSelectCommand,
  onToggleActionMenu,
  onDismissActionMenu,
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
    <CodexChatComposerFrame
      onLayout={onLayout}
      bottomPadding={bottomPadding}
      chrome={chrome}
      theme={theme}
    >
      <CodexComposerActionMenu
        visible={showCommandMenu}
        showComposerActions={showComposerActions}
        showCommandList={showCommandList}
        canAttach={canAttach}
        uploading={uploading}
        commands={commands}
        commandQuery={commandQuery}
        chrome={chrome}
        onUploadPress={() => {
          onUploadPress();
          onDismissActionMenu();
        }}
        onSelectCommand={(command) => {
          onSelectCommand(command);
          onDismissActionMenu();
        }}
      />

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
        uploading={uploading}
        sendEnabled={sendEnabled}
        sending={sending}
        sendIcon={sendIcon}
        sendLabel={sendLabel}
        sendElapsedLabel={sendElapsedLabel}
        running={running}
        actionMenuExpanded={showComposerActions}
        actionMenuButtonEnabled={composerActionButtonEnabled}
        chrome={chrome}
        theme={theme}
        onDraftChange={onDraftChange}
        onActionMenuPress={onToggleActionMenu}
        onInputPress={onInputPress}
        onInputFocus={onInputFocus}
        onInputBlur={onInputBlur}
        onInputStart={onInputStart}
        onSubmit={onSubmit}
        onSendPress={onSendPress}
      />
    </CodexChatComposerFrame>
  );
}
