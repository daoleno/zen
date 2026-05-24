import { useCallback } from "react";
import type { CodexSlashCommand } from "../../services/websocket";
import type { ComposerAttachment } from "./CodexChatSession";
import { requiresSlashCommandArgs } from "./CodexSlashCommands";

interface UseCodexSlashCommandPickerInput {
  attachments: ComposerAttachment[];
  setDraft(value: string): void;
  dismissActionMenu(): void;
  focusComposer(): void;
  startNewCodexChat(commandText?: string): void;
  sendSlashCommandToCodex(text: string): void;
  openSkillsSheet(): void;
  copyLastAssistantMessage(): void;
  showUnsupportedSlashCommand(command: CodexSlashCommand): void;
}

export function useCodexSlashCommandPicker({
  attachments,
  setDraft,
  dismissActionMenu,
  focusComposer,
  startNewCodexChat,
  sendSlashCommandToCodex,
  openSkillsSheet,
  copyLastAssistantMessage,
  showUnsupportedSlashCommand,
}: UseCodexSlashCommandPickerInput) {
  return useCallback(
    (command: CodexSlashCommand) => {
      dismissActionMenu();
      if (attachments.length > 0) {
        setDraft(`${command.value} `);
        focusComposer();
        return;
      }
      if (command.execution === "unsupported") {
        showUnsupportedSlashCommand(command);
        return;
      }
      if (!command.chat_supported) {
        setDraft(`${command.value} `);
        focusComposer();
        return;
      }
      if (command.execution === "insert-only") {
        setDraft(`${command.value} `);
        focusComposer();
        return;
      }
      if (command.execution === "native") {
        if (command.name === "new" || command.name === "clear") {
          startNewCodexChat(command.value);
          return;
        }
        if (command.name === "skills") {
          openSkillsSheet();
          return;
        }
        if (command.name === "copy") {
          copyLastAssistantMessage();
          return;
        }
        setDraft(`${command.value} `);
        focusComposer();
        return;
      }
      if (command.name === "new" || command.name === "clear") {
        startNewCodexChat(command.value);
        return;
      }
      if (!requiresSlashCommandArgs(command)) {
        sendSlashCommandToCodex(command.value);
        return;
      }
      setDraft(`${command.value} `);
      focusComposer();
    },
    [
      attachments,
      dismissActionMenu,
      focusComposer,
      openSkillsSheet,
      copyLastAssistantMessage,
      sendSlashCommandToCodex,
      setDraft,
      showUnsupportedSlashCommand,
      startNewCodexChat,
    ],
  );
}
