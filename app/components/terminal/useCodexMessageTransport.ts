import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type SetStateAction,
} from "react";
import { Alert, Keyboard } from "react-native";
import type { ConnectionState } from "../../store/agents";
import type { CodexSlashCommand } from "../../services/websocket";
import { wsClient } from "../../services/websocket";
import type {
  ComposerAttachment,
  PendingSlashCommandInput,
  PendingUserMessageInput,
} from "./CodexChatSession";

interface UseCodexMessageTransportInput {
  serverId: string;
  agentId: string;
  connectionState: ConnectionState;
  setDraft(value: string): void;
  setAttachments(value: SetStateAction<ComposerAttachment[]>): void;
  clearComposerNativeText(): void;
  addPendingUserMessage(message: PendingUserMessageInput): string;
  removePendingUserMessage(id: string): void;
  addPendingSlashCommand(command: PendingSlashCommandInput): string;
  settlePendingSlashCommand(id: string): void;
  removePendingSlashCommand(id: string): void;
  resetForNewChat(): void;
  markNewChatReady(): void;
  markNewChatMessageStarted(): void;
  scrollToLatest(animated?: boolean, delay?: number): void;
}

export function useCodexMessageTransport({
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
}: UseCodexMessageTransportInput) {
  const [sending, setSending] = useState(false);
  const [startingNewChat, setStartingNewChat] = useState(false);
  const [interrupting, setInterrupting] = useState(false);
  const sendLockedRef = useRef(false);
  const sendingResetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clearTransportTimers = useCallback(() => {
    if (sendingResetTimerRef.current) {
      clearTimeout(sendingResetTimerRef.current);
      sendingResetTimerRef.current = null;
    }
  }, []);

  const unlockSend = useCallback(() => {
    sendLockedRef.current = false;
    setSending(false);
  }, []);

  useEffect(
    () => () => {
      clearTransportTimers();
      sendLockedRef.current = false;
    },
    [clearTransportTimers],
  );

  const submitTextToCodex = useCallback(
    (
      text: string,
      previousDraft: string,
      previousAttachments: ComposerAttachment[],
    ) => {
      if (sendLockedRef.current) {
        return;
      }
      sendLockedRef.current = true;
      markNewChatMessageStarted();
      const pendingMessageId = addPendingUserMessage({
        body: previousDraft.trim(),
        sentText: text,
        attachments: previousAttachments.map((attachment) => ({
          name: attachment.name,
          path: attachment.path,
          localUri: attachment.localUri,
          mimeType: attachment.mimeType,
        })),
      });
      setSending(true);
      setDraft("");
      clearComposerNativeText();
      setAttachments([]);
      scrollToLatest(false, 0);
      try {
        clearTransportTimers();
        wsClient.sendInput(serverId, agentId, `${text}\n`);
        sendingResetTimerRef.current = setTimeout(() => {
          sendingResetTimerRef.current = null;
          unlockSend();
        }, 520);
      } catch (err: any) {
        clearTransportTimers();
        sendLockedRef.current = false;
        removePendingUserMessage(pendingMessageId);
        setDraft(previousDraft);
        setAttachments(previousAttachments);
        setSending(false);
        Alert.alert("Message not sent", err?.message || "Could not send this message.");
      }
    },
    [
      agentId,
      addPendingUserMessage,
      clearComposerNativeText,
      clearTransportTimers,
      markNewChatMessageStarted,
      removePendingUserMessage,
      scrollToLatest,
      serverId,
      setAttachments,
      setDraft,
      unlockSend,
    ],
  );

  const startNewCodexChat = useCallback((commandText: string = "/new") => {
    if (sendLockedRef.current) {
      return;
    }
    const submittedText = commandText.trim() || "/new";
    sendLockedRef.current = true;
    setSending(true);
    setStartingNewChat(true);
    Keyboard.dismiss();
    setDraft("");
    clearComposerNativeText();
    setAttachments([]);
    try {
      clearTransportTimers();
      resetForNewChat();
      wsClient.sendInput(serverId, agentId, `${submittedText}\n`);
      scrollToLatest(false, 0);
      sendingResetTimerRef.current = setTimeout(() => {
        sendingResetTimerRef.current = null;
        unlockSend();
        setStartingNewChat(false);
        markNewChatReady();
      }, 180);
    } catch (err: any) {
      clearTransportTimers();
      sendLockedRef.current = false;
      setSending(false);
      setStartingNewChat(false);
      Alert.alert("Command not sent", err?.message || "Could not start a new Codex chat.");
    }
  }, [
    agentId,
    clearComposerNativeText,
    clearTransportTimers,
    markNewChatReady,
    resetForNewChat,
    scrollToLatest,
    serverId,
    setAttachments,
    setDraft,
    unlockSend,
  ]);

  const sendSlashCommandToCodex = useCallback(
    (text: string, command?: CodexSlashCommand) => {
      if (sendLockedRef.current) {
        return;
      }
      sendLockedRef.current = true;
      setSending(true);
      Keyboard.dismiss();
      setDraft("");
      clearComposerNativeText();
      setAttachments([]);
      const commandName = command?.name || slashCommandNameFromText(text);
      const pendingCommandId = addPendingSlashCommand({
        text,
        name: commandName,
        title: command?.title,
        description: command?.description,
      });
      scrollToLatest(false, 0);

      try {
        clearTransportTimers();
        wsClient.sendInput(serverId, agentId, `${text}\n`);
        sendingResetTimerRef.current = setTimeout(() => {
          sendingResetTimerRef.current = null;
          settlePendingSlashCommand(pendingCommandId);
          unlockSend();
        }, 420);
      } catch (err: any) {
        clearTransportTimers();
        removePendingSlashCommand(pendingCommandId);
        unlockSend();
        Alert.alert("Command not sent", err?.message || "Could not send this command to Codex.");
      }
    },
    [
      agentId,
      addPendingSlashCommand,
      clearComposerNativeText,
      clearTransportTimers,
      removePendingSlashCommand,
      scrollToLatest,
      serverId,
      settlePendingSlashCommand,
      setAttachments,
      setDraft,
      unlockSend,
    ],
  );

  const interruptCodex = useCallback(() => {
    if (connectionState !== "connected" || sending) {
      return;
    }
    setInterrupting(true);
    setSending(true);
    try {
      wsClient.sendAction(serverId, agentId, "pause");
      setTimeout(() => {
        setInterrupting(false);
        setSending(false);
      }, 600);
    } catch {
      setInterrupting(false);
      setSending(false);
    }
  }, [agentId, connectionState, sending, serverId]);

  return {
    sending,
    interrupting,
    startingNewChat,
    submitTextToCodex,
    startNewCodexChat,
    sendSlashCommandToCodex,
    interruptCodex,
  };
}

function slashCommandNameFromText(text: string) {
  const match = /^\/([a-z][a-z0-9-]*)/.exec(text.trimStart());
  return match?.[1] || "command";
}
