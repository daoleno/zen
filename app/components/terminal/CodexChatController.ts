import {
  useCallback,
  useMemo,
  useState,
  type SetStateAction,
} from "react";
import { Alert } from "react-native";
import type { Agent, ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import { uploadDocumentForServer } from "../../services/uploads";
import { wsClient, type CodexSlashCommand } from "../../services/websocket";
import {
  type ChatCommandEvent,
  type ComposerAttachment,
} from "./CodexChatSession";
import {
  slashCommandTerminalText,
} from "./CodexSlashCommands";
import { SCROLL_TO_BOTTOM_LAYOUT_DELAY_MS } from "./CodexChatSurfaceHooks";
import {
  buildCodexComposerMessage,
  buildCodexStatusMeta,
} from "./CodexChatControllerModel";
import { useCodexNativeCommands } from "./useCodexNativeCommands";
import { useCodexSlashCommandRouter } from "./useCodexSlashCommandRouter";

const MAX_COMPOSER_ATTACHMENTS = 8;

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
  const [sending, setSending] = useState(false);
  const [uploading, setUploading] = useState(false);

  const statusMeta = useMemo(
    () =>
      buildCodexStatusMeta({
        agent,
        connectionState,
        connectionIssue,
        conversation,
        events,
        sending,
      }),
    [agent, connectionIssue, connectionState, conversation, events, sending],
  );

  const canAttach = connectionState === "connected" && !uploading;
  const canSend =
    connectionState === "connected" &&
    (draft.trim().length > 0 || attachments.length > 0) &&
    !sending &&
    !uploading;

  const submitTextToCodex = useCallback(
    (
      text: string,
      previousDraft: string,
      previousAttachments: ComposerAttachment[],
    ) => {
      setSending(true);
      setDraft("");
      setAttachments([]);
      scrollToLatest(true);
      try {
        wsClient.sendInput(serverId, agentId, `${text}\n`);
        setTimeout(() => {
          void refreshConversation(false);
          setSending(false);
        }, 600);
      } catch {
        setDraft(previousDraft);
        setAttachments(previousAttachments);
        setSending(false);
      }
    },
    [
      agentId,
      refreshConversation,
      scrollToLatest,
      serverId,
      setAttachments,
      setDraft,
    ],
  );

  const clearComposerForLocalCommand = useCallback(() => {
    setDraft("");
    setAttachments([]);
    scrollToLatest(true);
    setTimeout(() => {
      pinToBottomIfNeeded(true);
    }, SCROLL_TO_BOTTOM_LAYOUT_DELAY_MS);
  }, [pinToBottomIfNeeded, scrollToLatest, setAttachments, setDraft]);

  const openSlashCommandInTerminal = useCallback(
    (command: CodexSlashCommand, rawText?: string) => {
      const text = slashCommandTerminalText(command, rawText);
      const previousDraft = draft;
      const previousAttachments = attachments;
      setDraft("");
      setAttachments([]);
      try {
        wsClient.sendInput(serverId, agentId, `${text}\n`);
        recordChatCommandEvent({
          command,
          tone: "neutral",
          title: "Opened in Terminal",
          detail: command.value,
          body: command.interactive
            ? "This command uses the terminal renderer because it can open prompts, pickers, or terminal-only output."
            : "This command was routed to the terminal renderer.",
        });
        onSwitchToTerminal();
      } catch {
        setDraft(previousDraft);
        setAttachments(previousAttachments);
        recordChatCommandEvent({
          command,
          tone: "failed",
          title: "Command Not Sent",
          detail: command.value,
          body: "Zen could not send this command to the terminal session.",
        });
      }
    },
    [
      agentId,
      attachments,
      draft,
      onSwitchToTerminal,
      recordChatCommandEvent,
      serverId,
      setAttachments,
      setDraft,
    ],
  );

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

  const sendDraft = useCallback(() => {
    const text = buildCodexComposerMessage(draft, attachments);
    if (!text || connectionState !== "connected" || sending || uploading) {
      return;
    }
    const previousDraft = draft;
    const previousAttachments = attachments;
    if (
      routeDraftSubmission({
        draft,
        composedText: text,
        previousDraft,
        previousAttachments,
      })
    ) {
      return;
    }
    submitTextToCodex(text, previousDraft, previousAttachments);
  }, [
    attachments,
    connectionState,
    draft,
    routeDraftSubmission,
    sending,
    submitTextToCodex,
    uploading,
  ]);

  const interruptCodex = useCallback(() => {
    if (connectionState !== "connected" || sending) {
      return;
    }
    setSending(true);
    try {
      wsClient.sendAction(serverId, agentId, "pause");
      setTimeout(() => {
        void refreshConversation(false);
        setSending(false);
      }, 600);
    } catch {
      setSending(false);
    }
  }, [agentId, connectionState, refreshConversation, sending, serverId]);

  const handleUploadAttachment = useCallback(async () => {
    if (!canAttach) {
      return;
    }
    setUploading(true);
    try {
      const attachment = await uploadDocumentForServer(serverId);
      if (!attachment) {
        return;
      }
      setAttachments((current) =>
        [
          ...current,
          {
            ...attachment,
            id: `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`,
          },
        ].slice(-MAX_COMPOSER_ATTACHMENTS),
      );
      focusComposer();
    } catch (err: any) {
      Alert.alert("Upload failed", err?.message || "Could not upload this file.");
    } finally {
      setUploading(false);
    }
  }, [canAttach, focusComposer, serverId, setAttachments]);

  const removeAttachment = useCallback(
    (id: string) => {
      setAttachments((current) =>
        current.filter((attachment) => attachment.id !== id),
      );
    },
    [setAttachments],
  );

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
