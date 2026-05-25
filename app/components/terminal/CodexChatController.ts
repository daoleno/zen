import * as Clipboard from "expo-clipboard";
import {
  useCallback,
  type SetStateAction,
} from "react";
import { Alert } from "react-native";
import type { ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type {
  CodexSkill,
  CodexSlashCommand,
} from "../../services/websocket";
import {
  type ComposerAttachment,
  type PendingSlashCommandInput,
  type PendingUserMessageInput,
} from "./CodexChatSession";
import { useCodexComposerAttachments } from "./useCodexComposerAttachments";
import { useCodexControllerPresentation } from "./useCodexControllerPresentation";
import { useCodexDraftSubmission } from "./useCodexDraftSubmission";
import { useCodexMessageTransport } from "./useCodexMessageTransport";
import { useCodexSlashCommandRouter } from "./useCodexSlashCommandRouter";

interface UseCodexChatControllerInput {
  serverId: string;
  agentId: string;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  draft: string;
  setDraft(value: string): void;
  attachments: ComposerAttachment[];
  setAttachments(value: SetStateAction<ComposerAttachment[]>): void;
  slashCommands: CodexSlashCommand[];
  addPendingUserMessage(message: PendingUserMessageInput): string;
  removePendingUserMessage(id: string): void;
  addPendingSlashCommand(command: PendingSlashCommandInput): string;
  settlePendingSlashCommand(id: string): void;
  removePendingSlashCommand(id: string): void;
  resetForNewChat(): void;
  markNewChatReady(): void;
  markNewChatMessageStarted(): void;
  scrollToLatest(animated?: boolean, delay?: number): void;
  focusComposer(): void;
  dismissActionMenu(): void;
  openSkillsSheet(): void;
  openGitDiff(): void;
}

export function useCodexChatController({
  serverId,
  agentId,
  connectionState,
  connectionIssue,
  conversation,
  events,
  draft,
  setDraft,
  attachments,
  setAttachments,
  slashCommands,
  addPendingUserMessage,
  removePendingUserMessage,
  addPendingSlashCommand,
  settlePendingSlashCommand,
  removePendingSlashCommand,
  resetForNewChat,
  markNewChatReady,
  markNewChatMessageStarted,
  scrollToLatest,
  focusComposer,
  dismissActionMenu,
  openSkillsSheet,
  openGitDiff,
}: UseCodexChatControllerInput) {
  const insertSkillMention = useCallback((skill: CodexSkill) => {
    const mention = `$${skill.name}`;
    const nextDraft = draft.trim() && !draft.trimStart().startsWith("/")
      ? `${draft}${draft.endsWith(" ") || draft.endsWith("\n") ? "" : " "}${mention} `
      : `${mention} `;
    setDraft(nextDraft);
    focusComposer();
  }, [draft, focusComposer, setDraft]);

  const recordLocalSlashCommand = useCallback((
    command: CodexSlashCommand,
    text: string = command.value,
    completedTitle?: string,
  ) => {
    const id = addPendingSlashCommand({
      text,
      name: command.name,
      title: command.title,
      description: command.description,
      completedTitle,
    });
    settlePendingSlashCommand(id);
    scrollToLatest(false, 0);
  }, [addPendingSlashCommand, scrollToLatest, settlePendingSlashCommand]);

  const copyLastAssistantMessage = useCallback((command?: CodexSlashCommand) => {
    const lastAssistant = [...events]
      .reverse()
      .find(
        (event) =>
          (event.kind === "assistant_message" || event.kind === "status") &&
          Boolean(event.body?.trim()),
      );
    const text = lastAssistant?.body?.trim();
    if (!text) {
      Alert.alert("Nothing to copy", "There is no Codex response in this chat yet.");
      return;
    }
    void Clipboard.setStringAsync(text)
      .then(() => {
        if (command) {
          recordLocalSlashCommand(command, command.value, "Copied response");
        }
      })
      .catch((err: any) => {
        Alert.alert("Copy failed", err?.message || "Could not copy the last response.");
      });
  }, [events, recordLocalSlashCommand]);

  const {
    canAttach,
    handleUploadAttachment,
    removeAttachment,
    uploading,
  } = useCodexComposerAttachments({
    serverId,
    connectionState,
    setAttachments,
    focusComposer,
  });
  const {
    interruptCodex,
    interrupting,
    sending,
    startingNewChat,
    startNewCodexChat,
    sendSlashCommandToCodex,
    submitTextToCodex,
  } = useCodexMessageTransport({
    serverId,
    agentId,
    connectionState,
    setDraft,
    setAttachments,
    addPendingUserMessage,
    removePendingUserMessage,
    addPendingSlashCommand,
    settlePendingSlashCommand,
    removePendingSlashCommand,
    resetForNewChat,
    markNewChatReady,
    markNewChatMessageStarted,
    scrollToLatest,
  });

  const {
    statusMeta,
    canSend,
  } = useCodexControllerPresentation({
    connectionState,
    connectionIssue,
    conversation,
    events,
    draft,
    attachments,
    sending,
    uploading,
  });

  const {
    pickSlashCommand,
    routeDraftSubmission,
  } = useCodexSlashCommandRouter({
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
    openGitDiff,
    copyLastAssistantMessage,
  });

  const sendDraft = useCodexDraftSubmission({
    draft,
    attachments,
    connectionState,
    sending,
    uploading,
    routeDraftSubmission,
    submitTextToCodex,
  });

  return {
    sending,
    interrupting,
    startingNewChat,
    uploading,
    statusMeta,
    canAttach,
    canSend,
    sendDraft,
    interruptCodex,
    pickSlashCommand,
    handleUploadAttachment,
    removeAttachment,
    insertSkillMention,
  };
}
