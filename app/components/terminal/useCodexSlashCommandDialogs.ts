import { useCallback } from "react";
import { Alert } from "react-native";
import type { CodexSlashCommand } from "../../services/websocket";
import type { ComposerAttachment } from "./CodexChatSession";
import { slashCommandTerminalMessage } from "./CodexSlashCommands";

interface UseCodexSlashCommandDialogsInput {
  submitTextToCodex(
    text: string,
    previousDraft: string,
    previousAttachments: ComposerAttachment[],
  ): void;
  openSlashCommandInTerminal(
    command: CodexSlashCommand,
    rawText?: string,
  ): void;
}

export function useCodexSlashCommandDialogs({
  submitTextToCodex,
  openSlashCommandInTerminal,
}: UseCodexSlashCommandDialogsInput) {
  const showTerminalRequiredAction = useCallback(
    (
      command: CodexSlashCommand,
      rawText: string,
      composedText: string,
      previousDraft: string,
      previousAttachments: ComposerAttachment[],
    ) => {
      Alert.alert(
        `${command.value} needs Terminal`,
        slashCommandTerminalMessage(command),
        [
          { text: "Cancel", style: "cancel" },
          {
            text: "Send Anyway",
            onPress: () =>
              submitTextToCodex(
                composedText,
                previousDraft,
                previousAttachments,
              ),
          },
          {
            text: "Open Terminal",
            onPress: () => openSlashCommandInTerminal(command, rawText),
          },
        ],
      );
    },
    [openSlashCommandInTerminal, submitTextToCodex],
  );

  const showUnsupportedSlashCommand = useCallback(
    (command: CodexSlashCommand) => {
      Alert.alert(
        `${command.value} is not available`,
        "This command is hidden or internal in Codex and is not exposed in the chat renderer.",
        [{ text: "OK", style: "cancel" }],
      );
    },
    [],
  );

  const showUnknownSlashCommand = useCallback(
    (
      command: CodexSlashCommand,
      rawText: string,
      composedText: string,
      previousDraft: string,
      previousAttachments: ComposerAttachment[],
    ) => {
      Alert.alert(
        `${command.value} is not in the catalog`,
        "Zen cannot tell whether this slash command is interactive. Open it in Terminal, or send it as a normal message.",
        [
          { text: "Cancel", style: "cancel" },
          {
            text: "Send as Message",
            onPress: () =>
              submitTextToCodex(
                composedText,
                previousDraft,
                previousAttachments,
              ),
          },
          {
            text: "Open Terminal",
            onPress: () => openSlashCommandInTerminal(command, rawText),
          },
        ],
      );
    },
    [openSlashCommandInTerminal, submitTextToCodex],
  );

  const showSlashCommandAttachmentAlert = useCallback(
    (
      command: CodexSlashCommand,
      composedText: string,
      previousDraft: string,
      previousAttachments: ComposerAttachment[],
    ) => {
      Alert.alert(
        `${command.value} cannot use attachments here`,
        "Run the slash command without attachments, or send this as a normal message.",
        [
          { text: "Cancel", style: "cancel" },
          {
            text: "Send as Message",
            onPress: () =>
              submitTextToCodex(
                composedText,
                previousDraft,
                previousAttachments,
              ),
          },
        ],
      );
    },
    [submitTextToCodex],
  );

  return {
    showTerminalRequiredAction,
    showUnsupportedSlashCommand,
    showUnknownSlashCommand,
    showSlashCommandAttachmentAlert,
  };
}
