import { useCallback } from "react";
import type { CodexSlashCommand } from "../../services/websocket";
import type {
  ChatCommandEvent,
  ComposerAttachment,
} from "./CodexChatSession";
import {
  requiresSlashCommandArgs,
  slashCommandHasArgs,
  slashCommandRequestFromDraft,
  type SlashCommandRequest,
} from "./CodexSlashCommands";
import { useCodexSlashCommandDialogs } from "./useCodexSlashCommandDialogs";

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
  focusComposer(): void;
  recordChatCommandEvent(
    event: Omit<ChatCommandEvent, "id" | "createdAt">,
  ): void;
  submitTextToCodex(
    text: string,
    previousDraft: string,
    previousAttachments: ComposerAttachment[],
  ): void;
  openSlashCommandInTerminal(
    command: CodexSlashCommand,
    rawText?: string,
  ): void;
  runNativeSlashCommand(command: CodexSlashCommand): void | Promise<void>;
}

export function useCodexSlashCommandRouter({
  draft,
  attachments,
  slashCommands,
  setDraft,
  focusComposer,
  recordChatCommandEvent,
  submitTextToCodex,
  openSlashCommandInTerminal,
  runNativeSlashCommand,
}: UseCodexSlashCommandRouterInput) {
  const {
    showTerminalRequiredAction,
    showUnsupportedSlashCommand,
    showUnknownSlashCommand,
    showSlashCommandAttachmentAlert,
  } = useCodexSlashCommandDialogs({
    submitTextToCodex,
    openSlashCommandInTerminal,
  });

  const routeSlashCommandSubmission = useCallback(
    (
      request: SlashCommandRequest,
      composedText: string,
      previousDraft: string,
      previousAttachments: ComposerAttachment[],
    ) => {
      const { command, rawText, known } = request;
      if (previousAttachments.length > 0) {
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
        recordChatCommandEvent({
          command,
          tone: "failed",
          title: "Command Needs Input",
          detail: command.value,
          body: command.input.placeholder
            ? `Add arguments after ${command.value}: ${command.input.placeholder}`
            : `Add arguments after ${command.value}.`,
        });
        return true;
      }
      if (!known) {
        showUnknownSlashCommand(
          command,
          rawText,
          composedText,
          previousDraft,
          previousAttachments,
        );
        return true;
      }
      switch (command.execution) {
        case "chat-native":
        case "timeline-output":
          void runNativeSlashCommand(command);
          return true;
        case "terminal-required":
          showTerminalRequiredAction(
            command,
            rawText,
            composedText,
            previousDraft,
            previousAttachments,
          );
          return true;
        case "insert-only":
          return false;
        case "unsupported":
          showUnsupportedSlashCommand(command);
          return true;
        default:
          showTerminalRequiredAction(
            command,
            rawText,
            composedText,
            previousDraft,
            previousAttachments,
          );
          return true;
      }
    },
    [
      focusComposer,
      recordChatCommandEvent,
      runNativeSlashCommand,
      setDraft,
      showSlashCommandAttachmentAlert,
      showTerminalRequiredAction,
      showUnknownSlashCommand,
      showUnsupportedSlashCommand,
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
        return false;
      }
      return routeSlashCommandSubmission(
        slashRequest,
        composedText,
        previousDraft,
        previousAttachments,
      );
    },
    [routeSlashCommandSubmission, slashCommands],
  );

  const pickSlashCommand = useCallback(
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

  return {
    pickSlashCommand,
    routeDraftSubmission,
  };
}
