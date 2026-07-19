import { useCallback } from "react";
import type { CodexSlashCommand } from "../../services/websocket";
import type { ComposerAttachment } from "./InterfaceChatSession";
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
  attachments: ComposerAttachment[];
  slashCommands: CodexSlashCommand[];
  setDraft(value: string): void;
  dismissActionMenu(): void;
  focusComposer(): void;
  submitTextToInterface(
    text: string,
    previousDraft: string,
    previousAttachments: ComposerAttachment[],
  ): void;
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
  onSwitchToTerminal?: () => void;
}

export function useCodexSlashCommandRouter({
  attachments,
  slashCommands,
  setDraft,
  dismissActionMenu,
  focusComposer,
  submitTextToInterface,
  startNewInterfaceChat,
  sendSlashCommandToInterface,
  runStatusCommand,
  openSkillsSheet,
  onSwitchToTerminal,
}: UseCodexSlashCommandRouterInput) {
  const {
    showUnsupportedSlashCommand,
    showUnavailableSlashCommand,
    showSlashCommandAttachmentAlert,
  } = useCodexSlashCommandDialogs({
    submitTextToInterface,
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
      if (previousAttachments.length > 0 && command.execution !== "native") {
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
        runStatusCommand(rawText, command, previousDraft, previousAttachments);
        return true;
      }
      if (command.execution === "native") {
        if (command.name === "new" || command.name === "clear") {
          startNewInterfaceChat(rawText, previousDraft, previousAttachments);
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
        startNewInterfaceChat(rawText, previousDraft, previousAttachments);
        return true;
      }
      sendSlashCommandToInterface(
        rawText,
        command,
        previousDraft,
        previousAttachments,
      );
      return true;
    },
    [
      dismissActionMenu,
      focusComposer,
      sendSlashCommandToInterface,
      setDraft,
      showSlashCommandAttachmentAlert,
      showUnavailableSlashCommand,
      showUnsupportedSlashCommand,
      startNewInterfaceChat,
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
    startNewInterfaceChat,
    showUnsupportedSlashCommand,
    showUnavailableSlashCommand,
    sendSlashCommandToInterface,
    runStatusCommand,
    openSkillsSheet,
  });

  return {
    pickSlashCommand,
    routeDraftSubmission,
  };
}
