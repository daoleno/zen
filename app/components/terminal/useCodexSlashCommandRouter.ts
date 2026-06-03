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
  sendSlashCommandToCodex(text: string, command?: CodexSlashCommand): void;
  runStatusCommand(text: string, command?: CodexSlashCommand): void;
  openSkillsSheet(): void;
  onSwitchToTerminal(): void;
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
  runStatusCommand,
  openSkillsSheet,
  onSwitchToTerminal,
}: UseCodexSlashCommandRouterInput) {
  const {
    showUnsupportedSlashCommand,
    showUnavailableSlashCommand,
    showSlashCommandAttachmentAlert,
  } = useCodexSlashCommandDialogs({
    submitTextToCodex,
    onSwitchToTerminal,
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
      if (command.name === "status") {
        runStatusCommand(rawText, command);
        return true;
      }
      if (command.execution === "native") {
        if (command.name === "new" || command.name === "clear") {
          startNewCodexChat(rawText);
          return true;
        }
        if (command.name === "skills") {
          setDraft("");
          openSkillsSheet();
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
      sendSlashCommandToCodex(rawText, command);
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
      runStatusCommand,
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
    showUnavailableSlashCommand,
    sendSlashCommandToCodex,
    runStatusCommand,
    openSkillsSheet,
  });

  return {
    pickSlashCommand,
    routeDraftSubmission,
  };
}
