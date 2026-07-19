import { useCallback } from "react";
import type { CodexSlashCommand } from "../../services/websocket";
import type { ComposerAttachment } from "./InterfaceChatSession";
import { slashCommandAcceptsArgs } from "./CodexSlashCommands";

interface UseCodexSlashCommandPickerInput {
  attachments: ComposerAttachment[];
  setDraft(value: string): void;
  dismissActionMenu(): void;
  focusComposer(): void;
  startNewInterfaceChat(
    commandText?: string,
    previousDraft?: string,
    previousAttachments?: ComposerAttachment[],
  ): void;
  sendSlashCommandToInterface(
    text: string,
    command?: CodexSlashCommand,
    previousDraft?: string,
    previousAttachments?: ComposerAttachment[],
  ): void;
  runStatusCommand(
    text: string,
    command?: CodexSlashCommand,
    previousDraft?: string,
    previousAttachments?: ComposerAttachment[],
  ): void;
  openSkillsSheet(): void;
  showUnsupportedSlashCommand(command: CodexSlashCommand): void;
  showUnavailableSlashCommand(command: CodexSlashCommand): void;
}

export function useCodexSlashCommandPicker({
  attachments,
  setDraft,
  dismissActionMenu,
  focusComposer,
  startNewInterfaceChat,
  sendSlashCommandToInterface,
  runStatusCommand,
  openSkillsSheet,
  showUnsupportedSlashCommand,
  showUnavailableSlashCommand,
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
        showUnavailableSlashCommand(command);
        return;
      }
      if (command.execution === "insert-only") {
        setDraft(`${command.value} `);
        focusComposer();
        return;
      }
      if (command.name === "status") {
        runStatusCommand(command.value, command, command.value, []);
        return;
      }
      if (command.execution === "native") {
        if (command.name === "new" || command.name === "clear") {
          startNewInterfaceChat(command.value, command.value, []);
          return;
        }
        if (command.name === "skills") {
          setDraft("");
          openSkillsSheet();
          return;
        }
        setDraft(`${command.value} `);
        focusComposer();
        return;
      }
      if (command.name === "new" || command.name === "clear") {
        startNewInterfaceChat(command.value, command.value, []);
        return;
      }
      if (slashCommandAcceptsArgs(command)) {
        setDraft(`${command.value} `);
        focusComposer();
        return;
      }
      sendSlashCommandToInterface(command.value, command, command.value, []);
    },
    [
      attachments,
      dismissActionMenu,
      focusComposer,
      openSkillsSheet,
      runStatusCommand,
      sendSlashCommandToInterface,
      setDraft,
      showUnavailableSlashCommand,
      showUnsupportedSlashCommand,
      startNewInterfaceChat,
    ],
  );
}
