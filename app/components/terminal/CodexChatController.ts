import {
  useCallback,
  type SetStateAction,
} from "react";
import type { ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { AgentStatus } from "../../constants/tokens";
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
  agentStatus?: AgentStatus;
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
  clearComposerNativeText(): void;
  dismissActionMenu(): void;
  openStatusSheet(): void;
  openSkillsSheet(): void;
  onSwitchToTerminal(): void;
}

export function useCodexChatController({
  serverId,
  agentId,
  agentStatus,
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
  clearComposerNativeText,
  dismissActionMenu,
  openStatusSheet,
  openSkillsSheet,
  onSwitchToTerminal,
}: UseCodexChatControllerInput) {
  const insertSkillMention = useCallback((skill: CodexSkill) => {
    const mention = `$${skill.name}`;
    const nextDraft = draft.trim() && !draft.trimStart().startsWith("/")
      ? `${draft}${draft.endsWith(" ") || draft.endsWith("\n") ? "" : " "}${mention} `
      : `${mention} `;
    setDraft(nextDraft);
    focusComposer();
  }, [draft, focusComposer, setDraft]);

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
    clearComposerNativeText,
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
    agentStatus,
    draft,
    attachments,
    sending,
    uploading,
  });

  const runStatusCommand = useCallback((text: string, command?: CodexSlashCommand) => {
    openStatusSheet();
    sendSlashCommandToCodex(text, command);
  }, [openStatusSheet, sendSlashCommandToCodex]);

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
    runStatusCommand,
    openSkillsSheet,
    onSwitchToTerminal,
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
    runStatusCommand,
    handleUploadAttachment,
    removeAttachment,
    insertSkillMention,
  };
}
