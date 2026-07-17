import {
  useCallback,
  type SetStateAction,
} from "react";
import type { ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  ProviderActivity,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type {
  CodexSkill,
  CodexSlashCommand,
} from "../../services/websocket";
import {
  type ComposerAttachment,
  type PendingUserMessage,
  type PendingUserMessageAttempt,
  type PendingUserMessageInput,
  type PendingUserMessageRejection,
} from "./CodexChatSession";
import { useCodexComposerAttachments } from "./useCodexComposerAttachments";
import { useCodexControllerPresentation } from "./useCodexControllerPresentation";
import { useCodexDraftSubmission } from "./useCodexDraftSubmission";
import { useCodexMessageTransport } from "./useCodexMessageTransport";
import { useCodexSlashCommandRouter } from "./useCodexSlashCommandRouter";

interface UseCodexChatControllerInput {
  serverId: string;
  agentId: string;
  conversationScopeKey?: string;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  conversation: CodexConversation | null;
  runningActivity?: ProviderActivity;
  draft: string;
  setDraft(value: string): void;
  restoreDraft(value: string): void;
  attachments: ComposerAttachment[];
  pendingUserMessages: PendingUserMessage[];
  setAttachments(value: SetStateAction<ComposerAttachment[]>): void;
  slashCommands: CodexSlashCommand[];
  addPendingUserMessage(message: PendingUserMessageInput): string;
  beginPendingUserMessageAttempt(
    id: string,
    attempt: PendingUserMessageAttempt,
  ): void;
  rejectPendingUserMessage(
    id: string,
    rejection: PendingUserMessageRejection,
  ): void;
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
  connectionState,
  connectionIssue,
  conversation,
  runningActivity,
  draft,
  setDraft,
  restoreDraft,
  attachments,
  pendingUserMessages,
  setAttachments,
  slashCommands,
  addPendingUserMessage,
  beginPendingUserMessageAttempt,
  rejectPendingUserMessage,
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
  const {
    interruptCodex,
    interrupting,
    operationalError,
    retryPendingUserMessage,
    sending,
    startNewCodexChat,
    sendSlashCommandToCodex,
    submitTextToCodex,
  } = useCodexMessageTransport({
    serverId,
    agentId,
    conversationScopeKey,
    connectionState,
    runningActivity,
    draft,
    attachments,
    pendingUserMessages,
    setDraft,
    restoreDraft,
    setAttachments,
    clearComposerNativeText,
    addPendingUserMessage,
    beginPendingUserMessageAttempt,
    rejectPendingUserMessage,
    pinToBottomIfNeeded,
  });

  const {
    statusMeta,
    canSend,
  } = useCodexControllerPresentation({
    connectionState,
    connectionIssue,
    conversation,
    runningActivity,
    draft,
    attachments,
    sending,
    uploading,
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
