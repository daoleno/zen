import { useCallback, type SetStateAction } from "react";
import type { ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  ProviderActivity,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { CodexSkill, CodexSlashCommand } from "../../services/websocket";
import {
  type ComposerAttachment,
  type PendingUserMessage,
  type PendingUserMessageAttempt,
  type PendingUserMessageInput,
  type PendingUserMessageRejection,
} from "./InterfaceChatSession";
import { useInterfaceComposerAttachments } from "./useInterfaceComposerAttachments";
import { useInterfaceControllerPresentation } from "./useInterfaceControllerPresentation";
import { useInterfaceDraftSubmission } from "./useInterfaceDraftSubmission";
import { useInterfaceMessageTransport } from "./useInterfaceMessageTransport";
import { useCodexSlashCommandRouter } from "./useCodexSlashCommandRouter";

interface UseInterfaceChatControllerInput {
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
  requestTurnFocus(pendingMessageId: string): void;
  focusComposer(): void;
  clearComposerNativeText(): void;
  dismissActionMenu(): void;
  openStatusSheet(): void;
  openSkillsSheet(): void;
  onSwitchToTerminal?: () => void;
}

export function useInterfaceChatController({
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
  requestTurnFocus,
  focusComposer,
  clearComposerNativeText,
  dismissActionMenu,
  openStatusSheet,
  openSkillsSheet,
  onSwitchToTerminal,
}: UseInterfaceChatControllerInput) {
  const insertSkillMention = useCallback(
    (skill: CodexSkill) => {
      const mention = `$${skill.name}`;
      const nextDraft =
        draft.trim() && !draft.trimStart().startsWith("/")
          ? `${draft}${draft.endsWith(" ") || draft.endsWith("\n") ? "" : " "}${mention} `
          : `${mention} `;
      setDraft(nextDraft);
    },
    [draft, setDraft],
  );

  const {
    activeUpload,
    canAttach,
    cancelUpload,
    handleUploadAttachment,
    removeAttachment,
    uploading,
  } = useInterfaceComposerAttachments({
    serverId,
    connectionState,
    setAttachments,
    focusComposer,
  });
  const {
    interruptInterface,
    interrupting,
    operationalError,
    retryPendingUserMessage,
    sending,
    startNewInterfaceChat,
    sendSlashCommandToInterface,
    submitTextToInterface,
  } = useInterfaceMessageTransport({
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
    requestTurnFocus,
  });

  const { statusMeta, canSend } = useInterfaceControllerPresentation({
    connectionState,
    connectionIssue,
    conversation,
    runningActivity,
    draft,
    attachments,
    sending,
    uploading,
  });

  const runStatusCommand = useCallback(
    (
      text: string,
      command?: CodexSlashCommand,
      previousDraft?: string,
      previousAttachments?: ComposerAttachment[],
    ) => {
      openStatusSheet();
      sendSlashCommandToInterface(
        text,
        command,
        previousDraft,
        previousAttachments,
      );
    },
    [openStatusSheet, sendSlashCommandToInterface],
  );

  const { pickSlashCommand, routeDraftSubmission } = useCodexSlashCommandRouter(
    {
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
    },
  );

  const sendDraft = useInterfaceDraftSubmission({
    draft,
    attachments,
    connectionState,
    sending,
    uploading,
    routeDraftSubmission,
    submitTextToInterface,
  });

  return {
    activeUpload,
    sending,
    interrupting,
    uploading,
    statusMeta,
    operationalError,
    canAttach,
    canSend,
    sendDraft,
    interruptInterface,
    retryPendingUserMessage,
    pickSlashCommand,
    runStatusCommand,
    handleUploadAttachment,
    cancelUpload,
    removeAttachment,
    insertSkillMention,
  };
}
