import React from "react";
import {
  StyleSheet,
  Text,
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
  operationalError?: string;
  sendLabel: string;
  showStopButton: boolean;
  stopEnabled: boolean;
  stopLabel: string;
  stopLoading: boolean;
  providerActivityStartedAt?: string;
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
  operationalError,
  sendLabel,
  showStopButton,
  stopEnabled,
  stopLabel,
  stopLoading,
  providerActivityStartedAt,
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

      {operationalError ? (
        <Text
          accessibilityLiveRegion="polite"
          accessibilityRole="alert"
          style={[styles.operationalError, { color: chrome.danger }]}
        >
          {operationalError}
        </Text>
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
        sendLabel={sendLabel}
        showStopButton={showStopButton}
        stopEnabled={stopEnabled}
        stopLabel={stopLabel}
        stopLoading={stopLoading}
        providerActivityStartedAt={providerActivityStartedAt}
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
  operationalError: {
    marginHorizontal: 8,
    marginBottom: 4,
    fontSize: 12,
    lineHeight: 16,
  },
});
