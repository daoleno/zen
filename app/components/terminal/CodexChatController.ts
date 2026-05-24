import * as Clipboard from "expo-clipboard";
import {
  useCallback,
  type SetStateAction,
} from "react";
import { Alert } from "react-native";
import type { Agent, ConnectionState } from "../../store/agents";
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
  agent?: Agent;
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
  startPendingAssistantMessage(sentText: string, baselineLines: string[]): string;
  resetForNewChat(): void;
  markNewChatReady(): void;
  markNewChatMessageStarted(): void;
  refreshConversation(showLoading?: boolean): Promise<void>;
  scrollToLatest(animated?: boolean, delay?: number): void;
  focusComposer(): void;
  dismissActionMenu(): void;
  openSkillsSheet(): void;
}

export function useCodexChatController({
  serverId,
  agentId,
  agent,
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
  startPendingAssistantMessage,
  resetForNewChat,
  markNewChatReady,
  markNewChatMessageStarted,
  refreshConversation,
  scrollToLatest,
  focusComposer,
  dismissActionMenu,
  openSkillsSheet,
}: UseCodexChatControllerInput) {
  const insertSkillMention = useCallback((skill: CodexSkill) => {
    const mention = `$${skill.name}`;
    const nextDraft = draft.trim() && !draft.trimStart().startsWith("/")
      ? `${draft}${draft.endsWith(" ") || draft.endsWith("\n") ? "" : " "}${mention} `
      : `${mention} `;
    setDraft(nextDraft);
    focusComposer();
  }, [draft, focusComposer, setDraft]);

  const copyLastAssistantMessage = useCallback(() => {
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
    void Clipboard.setStringAsync(text).catch((err: any) => {
      Alert.alert("Copy failed", err?.message || "Could not copy the last response.");
    });
  }, [events]);

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
    startPendingAssistantMessage,
    terminalBaselineLines: agent?.last_output_lines ?? [],
    resetForNewChat,
    markNewChatReady,
    markNewChatMessageStarted,
    refreshConversation,
    scrollToLatest,
  });

  const {
    statusMeta,
    canSend,
  } = useCodexControllerPresentation({
    agent,
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
