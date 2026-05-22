import {
  type SetStateAction,
} from "react";
import type { Agent, ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { CodexSlashCommand } from "../../services/websocket";
import {
  type ChatCommandEvent,
  type ComposerAttachment,
} from "./CodexChatSession";
import { useCodexComposerAttachments } from "./useCodexComposerAttachments";
import { useCodexControllerPresentation } from "./useCodexControllerPresentation";
import { useCodexDraftSubmission } from "./useCodexDraftSubmission";
import { useCodexMessageTransport } from "./useCodexMessageTransport";
import { useCodexNativeCommands } from "./useCodexNativeCommands";
import { useCodexSlashCommandRouter } from "./useCodexSlashCommandRouter";
import { useCodexTerminalCommandActions } from "./useCodexTerminalCommandActions";

interface GitDiffAction {
  label: string;
  tone: "clean" | "dirty" | "error" | "loading";
  onPress(): void;
}

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
  gitDiff?: GitDiffAction | null;
  onSwitchToTerminal(): void;
  onOpenGitDiff?: () => void;
  recordChatCommandEvent(
    event: Omit<ChatCommandEvent, "id" | "createdAt">,
  ): void;
  refreshConversation(showLoading?: boolean): Promise<void>;
  scrollToLatest(animated?: boolean, delay?: number): void;
  pinToBottomIfNeeded(animated?: boolean, delay?: number): void;
  focusComposer(): void;
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
  gitDiff,
  onSwitchToTerminal,
  onOpenGitDiff,
  recordChatCommandEvent,
  refreshConversation,
  scrollToLatest,
  pinToBottomIfNeeded,
  focusComposer,
}: UseCodexChatControllerInput) {
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
    sending,
    submitTextToCodex,
  } = useCodexMessageTransport({
    serverId,
    agentId,
    connectionState,
    setDraft,
    setAttachments,
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
    clearComposerForLocalCommand,
    openSlashCommandInTerminal,
  } = useCodexTerminalCommandActions({
    serverId,
    agentId,
    draft,
    attachments,
    setDraft,
    setAttachments,
    recordChatCommandEvent,
    scrollToLatest,
    pinToBottomIfNeeded,
    onSwitchToTerminal,
  });

  const runNativeSlashCommand = useCodexNativeCommands({
    agent,
    connectionState,
    connectionIssue,
    conversation,
    events,
    slashCommands,
    statusMeta,
    gitDiff,
    onOpenGitDiff,
    clearComposerForLocalCommand,
    recordChatCommandEvent,
  });

  const {
    pickSlashCommand,
    routeDraftSubmission,
  } = useCodexSlashCommandRouter({
    draft,
    attachments,
    slashCommands,
    setDraft,
    focusComposer,
    recordChatCommandEvent,
    submitTextToCodex,
    openSlashCommandInTerminal,
    runNativeSlashCommand,
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
    uploading,
    statusMeta,
    canAttach,
    canSend,
    sendDraft,
    interruptCodex,
    pickSlashCommand,
    handleUploadAttachment,
    removeAttachment,
  };
}
