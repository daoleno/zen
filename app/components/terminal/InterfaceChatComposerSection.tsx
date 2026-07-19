import React from "react";
import type { TextInput } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { CodexSlashCommand } from "../../services/websocket";
import type { ComposerAttachment } from "./InterfaceChatSession";
import type { InterfaceComposerPresentation } from "./InterfaceChatSurfaceModel";
import { InterfaceChatComposer } from "./InterfaceChatComposer";

interface InterfaceChatComposerSectionProps {
  inputRef: React.RefObject<TextInput | null>;
  draft: string;
  editable: boolean;
  focused: boolean;
  canAttach: boolean;
  uploading: boolean;
  sending: boolean;
  operationalError?: string;
  attachments: ComposerAttachment[];
  presentation: InterfaceComposerPresentation;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onSelectCommand(command: CodexSlashCommand): void;
  onToggleActionMenu(): void;
  onDismissActionMenu(): void;
  onRemoveAttachment(id: string): void;
  onDraftChange(value: string): void;
  onUploadPress(): void;
  onInputFocus(): void;
  onInputBlur(): void;
  onSendPress(): void;
  onStopPress(): void;
}

export function InterfaceChatComposerSection({
  inputRef,
  draft,
  editable,
  focused,
  canAttach,
  uploading,
  sending,
  operationalError,
  attachments,
  presentation,
  chrome,
  theme,
  onSelectCommand,
  onToggleActionMenu,
  onDismissActionMenu,
  onRemoveAttachment,
  onDraftChange,
  onUploadPress,
  onInputFocus,
  onInputBlur,
  onSendPress,
  onStopPress,
}: InterfaceChatComposerSectionProps) {
  return (
    <InterfaceChatComposer
      inputRef={inputRef}
      draft={draft}
      placeholder={presentation.placeholder}
      editable={editable}
      focused={focused}
      canAttach={canAttach}
      uploading={uploading}
      sendEnabled={presentation.sendEnabled}
      sending={sending}
      operationalError={operationalError}
      sendLabel={presentation.sendLabel}
      showStopButton={presentation.showStopButton}
      stopEnabled={presentation.stopEnabled}
      stopLabel={presentation.stopLabel}
      stopLoading={presentation.stopLoading}
      providerActivityStartedAt={presentation.providerActivityStartedAt}
      bottomPadding={presentation.bottomPadding}
      showActionMenuButton={presentation.showActionMenuButton}
      actionMenuIcon={presentation.actionMenuIcon}
      composerLayout={presentation.composerLayout}
      showAttachmentRail={presentation.showAttachmentRail}
      showCommandMenu={presentation.showCommandMenu}
      showCommandList={presentation.showCommandList}
      showComposerActions={presentation.showComposerActions}
      composerActionButtonEnabled={presentation.composerActionButtonEnabled}
      commandQuery={presentation.commandQuery}
      commands={presentation.visibleSlashCommands}
      attachments={attachments}
      chrome={chrome}
      theme={theme}
      onSelectCommand={onSelectCommand}
      onToggleActionMenu={onToggleActionMenu}
      onDismissActionMenu={onDismissActionMenu}
      onRemoveAttachment={onRemoveAttachment}
      onDraftChange={onDraftChange}
      onUploadPress={onUploadPress}
      onInputFocus={onInputFocus}
      onInputBlur={onInputBlur}
      onSendPress={onSendPress}
      onStopPress={onStopPress}
    />
  );
}
