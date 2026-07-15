import {
  useCallback,
  type SetStateAction,
} from "react";
import type { ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  CodexConversationEvent,
  StructuredTurn,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { AgentStatus } from "../../constants/tokens";
import type {
  CodexSkill,
  CodexSlashCommand,
} from "../../services/websocket";
import {
  type ComposerAttachment,
  type PendingUserMessage,
  type PendingUserMessageAcknowledgement,
  type PendingUserMessageDispatchAttempt,
  type PendingUserMessageInput,
  type PendingUserMessageRejection,
} from "./CodexChatSession";
import { isCodexRequestRunning } from "./CodexChatControllerModel";
import { useCodexComposerAttachments } from "./useCodexComposerAttachments";
import { useCodexControllerPresentation } from "./useCodexControllerPresentation";
import { useCodexDraftSubmission } from "./useCodexDraftSubmission";
import { useCodexMessageTransport } from "./useCodexMessageTransport";
import { useCodexSlashCommandRouter } from "./useCodexSlashCommandRouter";
import { structuredConversationClientIdentity } from "./structuredTurnLifecycle";

interface UseCodexChatControllerInput {
  serverId: string;
  agentId: string;
  conversationScopeKey?: string;
  agentStatus?: AgentStatus;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  turnBusy?: boolean;
  workingTurn?: StructuredTurn;
  draft: string;
  setDraft(value: string): void;
  restoreDraft(value: string): void;
  attachments: ComposerAttachment[];
  pendingUserMessages: PendingUserMessage[];
  setAttachments(value: SetStateAction<ComposerAttachment[]>): void;
  slashCommands: CodexSlashCommand[];
  addPendingUserMessage(message: PendingUserMessageInput): string;
  acknowledgePendingUserMessage(
    id: string,
    acknowledgement: PendingUserMessageAcknowledgement,
  ): void;
  markPendingUserMessageDispatched(
    id: string,
    attempt: PendingUserMessageDispatchAttempt,
  ): void;
  rejectPendingUserMessage(
    id: string,
    rejection: PendingUserMessageRejection,
  ): void;
  markNewChatMessageStarted(): void;
  pinToBottomIfNeeded(animated?: boolean, delay?: number): void;
  focusComposer(): void;
  clearComposerNativeText(): void;
  dismissActionMenu(): void;
  openStatusSheet(): void;
  openSkillsSheet(): void;
  onSwitchToTerminal?: () => void;
}

export function useCodexChatController({
  serverId,
  agentId,
  conversationScopeKey,
  agentStatus,
  connectionState,
  connectionIssue,
  conversation,
  events,
  turnBusy,
  workingTurn,
  draft,
  setDraft,
  restoreDraft,
  attachments,
  pendingUserMessages,
  setAttachments,
  slashCommands,
  addPendingUserMessage,
  acknowledgePendingUserMessage,
  markPendingUserMessageDispatched,
  rejectPendingUserMessage,
  markNewChatMessageStarted,
  pinToBottomIfNeeded,
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
  const requestTurnBusy =
    turnBusy ??
    isCodexRequestRunning({
      conversation,
      events,
      agentStatus,
    });
  const {
    interruptCodex,
    interrupting,
    operationalError,
    retryPendingUserMessage,
    sending,
    startingNewChat,
    startNewCodexChat,
    sendSlashCommandToCodex,
    submitTextToCodex,
  } = useCodexMessageTransport({
    serverId,
    agentId,
    conversationScopeKey,
    conversationIdentity: structuredConversationClientIdentity(conversation),
    connectionState,
    turnBusy: requestTurnBusy,
    workingTurn,
    draft,
    attachments,
    pendingUserMessages,
    setDraft,
    restoreDraft,
    setAttachments,
    clearComposerNativeText,
    addPendingUserMessage,
    acknowledgePendingUserMessage,
    markPendingUserMessageDispatched,
    rejectPendingUserMessage,
    markNewChatMessageStarted,
    pinToBottomIfNeeded,
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
    requestRunning: requestTurnBusy,
  });

  const runStatusCommand = useCallback((
    text: string,
    command?: CodexSlashCommand,
    previousDraft?: string,
    previousAttachments?: ComposerAttachment[],
  ) => {
    openStatusSheet();
    sendSlashCommandToCodex(
      text,
      command,
      previousDraft,
      previousAttachments,
    );
  }, [openStatusSheet, sendSlashCommandToCodex]);

  const {
    pickSlashCommand,
    routeDraftSubmission,
  } = useCodexSlashCommandRouter({
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
    operationalError,
    canAttach,
    canSend,
    sendDraft,
    interruptCodex,
    retryPendingUserMessage,
    pickSlashCommand,
    runStatusCommand,
    handleUploadAttachment,
    removeAttachment,
    insertSkillMention,
  };
}
