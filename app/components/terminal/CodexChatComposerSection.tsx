import React from "react";
import type {
  LayoutChangeEvent,
  TextInput,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { CodexSlashCommand } from "../../services/websocket";
import type { ComposerAttachment } from "./CodexChatSession";
import type { CodexComposerPresentation } from "./CodexChatSurfaceModel";
import { CodexChatComposer } from "./CodexChatComposer";

interface CodexChatComposerSectionProps {
  inputRef: React.RefObject<TextInput | null>;
  draft: string;
  editable: boolean;
  focused: boolean;
  canAttach: boolean;
  uploading: boolean;
  sending: boolean;
  attachments: ComposerAttachment[];
  presentation: CodexComposerPresentation;
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

export function CodexChatComposerSection({
  inputRef,
  draft,
  editable,
  focused,
  canAttach,
  uploading,
  sending,
  attachments,
  presentation,
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
}: CodexChatComposerSectionProps) {
  return (
    <CodexChatComposer
      inputRef={inputRef}
      draft={draft}
      placeholder={presentation.placeholder}
      editable={editable}
      focused={focused}
      floating={presentation.active}
      canAttach={canAttach}
      uploading={uploading}
      sendEnabled={presentation.sendEnabled}
      sending={sending}
      sendIcon={presentation.sendIcon}
      sendLabel={presentation.sendLabel}
      running={presentation.showStopIndicator}
      bottomPadding={presentation.bottomPadding}
      showCommandMenu={presentation.showCommandMenu}
      commandQuery={presentation.commandQuery}
      commands={presentation.visibleSlashCommands}
      attachments={attachments}
      chrome={chrome}
      theme={theme}
      onLayout={onLayout}
      onSelectCommand={onSelectCommand}
      onRemoveAttachment={onRemoveAttachment}
      onDraftChange={onDraftChange}
      onUploadPress={onUploadPress}
      onInputPress={onInputPress}
      onInputFocus={onInputFocus}
      onInputBlur={onInputBlur}
      onInputStart={onInputStart}
      onSubmit={onSubmit}
      onSendPress={onSendPress}
    />
  );
}
