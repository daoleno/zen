import { useCallback } from "react";
import type { CodexSlashCommand } from "../../services/websocket";
import type { ComposerAttachment } from "./CodexChatSession";
import {
  requiresSlashCommandArgs,
  slashCommandHasArgs,
  slashCommandRequestFromDraft,
  type SlashCommandRequest,
} from "./CodexSlashCommands";
import { useCodexSlashCommandDialogs } from "./useCodexSlashCommandDialogs";
import { useCodexSlashCommandPicker } from "./useCodexSlashCommandPicker";

interface RouteDraftSubmissionInput {
  draft: string;
  composedText: string;
  previousDraft: string;
  previousAttachments: ComposerAttachment[];
}

interface UseCodexSlashCommandRouterInput {
  draft: string;
  attachments: ComposerAttachment[];
  slashCommands: CodexSlashCommand[];
  setDraft(value: string): void;
  dismissActionMenu(): void;
  focusComposer(): void;
  submitTextToCodex(
    text: string,
    previousDraft: string,
    previousAttachments: ComposerAttachment[],
  ): void;
  startNewCodexChat(commandText?: string): void;
  sendSlashCommandToCodex(text: string): void;
  openSkillsSheet(): void;
  copyLastAssistantMessage(): void;
}

export function useCodexSlashCommandRouter({
  draft,
  attachments,
  slashCommands,
  setDraft,
  dismissActionMenu,
  focusComposer,
  submitTextToCodex,
  startNewCodexChat,
  sendSlashCommandToCodex,
  openSkillsSheet,
  copyLastAssistantMessage,
}: UseCodexSlashCommandRouterInput) {
  const {
    showUnsupportedSlashCommand,
    showUnavailableSlashCommand,
    showSlashCommandAttachmentAlert,
  } = useCodexSlashCommandDialogs({
    submitTextToCodex,
  });

  const routeSlashCommandSubmission = useCallback(
    (
      request: SlashCommandRequest,
      composedText: string,
      previousDraft: string,
      previousAttachments: ComposerAttachment[],
    ) => {
      const { command, rawText, known } = request;
      dismissActionMenu();
      if (
        previousAttachments.length > 0 &&
        command.execution !== "native"
      ) {
        showSlashCommandAttachmentAlert(
          command,
          composedText,
          previousDraft,
          previousAttachments,
        );
        return true;
      }
      if (
        requiresSlashCommandArgs(command) &&
        !slashCommandHasArgs(rawText, command)
      ) {
        setDraft(`${command.value} `);
        focusComposer();
        return true;
      }
      if (!known) {
        setDraft(`${command.value} `);
        focusComposer();
        showUnavailableSlashCommand(command);
        return true;
      }
      if (!command.chat_supported) {
        setDraft(rawText);
        focusComposer();
        showUnavailableSlashCommand(command);
        return true;
      }
      if (command.execution === "unsupported") {
        showUnsupportedSlashCommand(command);
        return true;
      }
      if (command.execution === "insert-only") {
        return false;
      }
      if (command.execution === "native") {
        if (command.name === "new" || command.name === "clear") {
          startNewCodexChat(rawText);
          return true;
        }
        if (command.name === "skills") {
          openSkillsSheet();
          return true;
        }
        if (command.name === "copy") {
          copyLastAssistantMessage();
          return true;
        }
        setDraft(`${command.value} `);
        focusComposer();
        return true;
      }
      if (command.name === "new" || command.name === "clear") {
        startNewCodexChat(rawText);
        return true;
      }
      sendSlashCommandToCodex(rawText);
      return true;
    },
    [
      dismissActionMenu,
      focusComposer,
      sendSlashCommandToCodex,
      setDraft,
      showSlashCommandAttachmentAlert,
      showUnavailableSlashCommand,
      showUnsupportedSlashCommand,
      startNewCodexChat,
      openSkillsSheet,
      copyLastAssistantMessage,
    ],
  );

  const routeDraftSubmission = useCallback(
    ({
      draft,
      composedText,
      previousDraft,
      previousAttachments,
    }: RouteDraftSubmissionInput) => {
      const slashRequest = slashCommandRequestFromDraft(draft, slashCommands);
      if (!slashRequest) {
        dismissActionMenu();
        return false;
      }
      return routeSlashCommandSubmission(
        slashRequest,
        composedText,
        previousDraft,
        previousAttachments,
      );
    },
    [dismissActionMenu, routeSlashCommandSubmission, slashCommands],
  );

  const pickSlashCommand = useCodexSlashCommandPicker({
    attachments,
    setDraft,
    dismissActionMenu,
    focusComposer,
    startNewCodexChat,
    showUnsupportedSlashCommand,
    sendSlashCommandToCodex,
    openSkillsSheet,
    copyLastAssistantMessage,
  });

  return {
    pickSlashCommand,
    routeDraftSubmission,
  };
}
