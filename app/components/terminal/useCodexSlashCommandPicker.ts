import { useCallback } from "react";
import type { CodexSlashCommand } from "../../services/websocket";
import type { ComposerAttachment } from "./CodexChatSession";
import { requiresSlashCommandArgs } from "./CodexSlashCommands";

interface UseCodexSlashCommandPickerInput {
  draft: string;
  attachments: ComposerAttachment[];
  setDraft(value: string): void;
  focusComposer(): void;
  showTerminalRequiredAction(
    command: CodexSlashCommand,
    rawText: string,
    composedText: string,
    previousDraft: string,
    previousAttachments: ComposerAttachment[],
  ): void;
  showUnsupportedSlashCommand(command: CodexSlashCommand): void;
  runNativeSlashCommand(command: CodexSlashCommand): void | Promise<void>;
}

export function useCodexSlashCommandPicker({
  draft,
  attachments,
  setDraft,
  focusComposer,
  showTerminalRequiredAction,
  showUnsupportedSlashCommand,
  runNativeSlashCommand,
}: UseCodexSlashCommandPickerInput) {
  return useCallback(
    (command: CodexSlashCommand) => {
      if (attachments.length > 0) {
        setDraft(`${command.value} `);
        focusComposer();
        return;
      }
      if (command.execution === "unsupported") {
        showUnsupportedSlashCommand(command);
        return;
      }
      if (
        command.execution === "chat-native" &&
        !requiresSlashCommandArgs(command)
      ) {
        void runNativeSlashCommand(command);
        return;
      }
      if (
        command.execution === "terminal-required" &&
        !requiresSlashCommandArgs(command)
      ) {
        showTerminalRequiredAction(
          command,
          command.value,
          command.value,
          draft,
          attachments,
        );
        return;
      }
      setDraft(`${command.value} `);
      focusComposer();
    },
    [
      attachments,
      draft,
      focusComposer,
      runNativeSlashCommand,
      setDraft,
      showTerminalRequiredAction,
      showUnsupportedSlashCommand,
    ],
  );
}
