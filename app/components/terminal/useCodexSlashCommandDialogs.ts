import { useCallback } from "react";
import { Alert } from "react-native";
import type { CodexSlashCommand } from "../../services/websocket";
import type { ComposerAttachment } from "./InterfaceChatSession";

interface UseCodexSlashCommandDialogsInput {
  submitTextToInterface(
    text: string,
    previousDraft: string,
    previousAttachments: ComposerAttachment[],
  ): void;
  onSwitchToTerminal?: () => void;
}

export function useCodexSlashCommandDialogs({
  submitTextToInterface,
  onSwitchToTerminal,
}: UseCodexSlashCommandDialogsInput) {
  const showUnsupportedSlashCommand = useCallback(
    (command: CodexSlashCommand) => {
      Alert.alert(
        `${command.value} is not available`,
        "This command is hidden or internal in Codex.",
        [{ text: "OK", style: "cancel" }],
      );
    },
    [],
  );

  const showUnavailableSlashCommand = useCallback(
    (command: CodexSlashCommand) => {
      Alert.alert(
        `${command.value} is not available`,
        "This command opens an interactive Codex control that is not available in ChatUI yet.",
        onSwitchToTerminal
          ? [
              { text: "Cancel", style: "cancel" },
              { text: "Open Terminal", onPress: onSwitchToTerminal },
            ]
          : [{ text: "OK", style: "cancel" }],
      );
    },
    [onSwitchToTerminal],
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
              submitTextToInterface(
                composedText,
                previousDraft,
                previousAttachments,
              ),
          },
        ],
      );
    },
    [submitTextToInterface],
  );

  return {
    showUnsupportedSlashCommand,
    showUnavailableSlashCommand,
    showSlashCommandAttachmentAlert,
  };
}
