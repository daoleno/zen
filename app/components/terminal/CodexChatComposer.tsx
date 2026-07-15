import React from "react";
import {
  type LayoutChangeEvent,
  StyleSheet,
  View,
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
  showStopButton: boolean;
  stopEnabled: boolean;
  stopLabel: string;
  stopLoading: boolean;
  workingTurnStartedAt?: string;
  bottomPadding: number;
  showActionMenuButton: boolean;
  actionMenuIcon: "add" | "happy-outline";
  composerLayout: "chatgpt" | "telegram" | "classic";
  showAttachmentRail: boolean;
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
  onInputFocus(): void;
  onInputBlur(): void;
  onSendPress(): void;
  onStopPress(): void;
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
  showStopButton,
  stopEnabled,
  stopLabel,
  stopLoading,
  workingTurnStartedAt,
  bottomPadding,
  showActionMenuButton,
  actionMenuIcon,
  composerLayout,
  showAttachmentRail,
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
  onInputFocus,
  onInputBlur,
  onSendPress,
  onStopPress,
}: CodexChatComposerProps) {
  return (
    <CodexChatComposerFrame
      onLayout={onLayout}
      bottomPadding={bottomPadding}
      chrome={chrome}
      theme={theme}
      composerLayout={composerLayout}
    >
      {showCommandMenu ? (
        <View style={styles.menuSlot}>
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
        </View>
      ) : null}

      {showAttachmentRail ? (
        <CodexComposerAttachmentRail
          attachments={attachments}
          uploading={uploading}
          chrome={chrome}
          onRemoveAttachment={onRemoveAttachment}
        />
      ) : null}

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
        showStopButton={showStopButton}
        stopEnabled={stopEnabled}
        stopLabel={stopLabel}
        stopLoading={stopLoading}
        workingTurnStartedAt={workingTurnStartedAt}
        actionMenuExpanded={showComposerActions}
        actionMenuButtonEnabled={composerActionButtonEnabled}
        showActionMenuButton={showActionMenuButton}
        actionMenuIcon={actionMenuIcon}
        composerLayout={composerLayout}
        chrome={chrome}
        theme={theme}
        onDraftChange={onDraftChange}
        onActionMenuPress={onToggleActionMenu}
        onInputFocus={onInputFocus}
        onInputBlur={onInputBlur}
        onSendPress={onSendPress}
        onStopPress={onStopPress}
      />
    </CodexChatComposerFrame>
  );
}

const styles = StyleSheet.create({
  menuSlot: {
    marginBottom: 8,
    zIndex: 6,
  },
});
