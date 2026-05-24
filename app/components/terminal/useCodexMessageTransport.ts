import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type SetStateAction,
} from "react";
import { Alert, Keyboard } from "react-native";
import type { ConnectionState } from "../../store/agents";
import { wsClient } from "../../services/websocket";
import type {
  ComposerAttachment,
  PendingUserMessageInput,
} from "./CodexChatSession";

interface UseCodexMessageTransportInput {
  serverId: string;
  agentId: string;
  connectionState: ConnectionState;
  setDraft(value: string): void;
  setAttachments(value: SetStateAction<ComposerAttachment[]>): void;
  addPendingUserMessage(message: PendingUserMessageInput): string;
  removePendingUserMessage(id: string): void;
  startPendingAssistantMessage(sentText: string, baselineLines: string[]): string;
  terminalBaselineLines: string[];
  resetForNewChat(): void;
  markNewChatReady(): void;
  markNewChatMessageStarted(): void;
  refreshConversation(showLoading?: boolean): Promise<void>;
  scrollToLatest(animated?: boolean, delay?: number): void;
}

export function useCodexMessageTransport({
  serverId,
  agentId,
  connectionState,
  setDraft,
  setAttachments,
  addPendingUserMessage,
  removePendingUserMessage,
  startPendingAssistantMessage,
  terminalBaselineLines,
  resetForNewChat,
  markNewChatReady,
  markNewChatMessageStarted,
  refreshConversation,
  scrollToLatest,
}: UseCodexMessageTransportInput) {
  const [sending, setSending] = useState(false);
  const [startingNewChat, setStartingNewChat] = useState(false);
  const [interrupting, setInterrupting] = useState(false);
  const sendLockedRef = useRef(false);
  const sendingResetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const refreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clearTransportTimers = useCallback(() => {
    if (sendingResetTimerRef.current) {
      clearTimeout(sendingResetTimerRef.current);
      sendingResetTimerRef.current = null;
    }
    if (refreshTimerRef.current) {
      clearTimeout(refreshTimerRef.current);
      refreshTimerRef.current = null;
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
        })),
      });
      setSending(true);
      setDraft("");
      setAttachments([]);
      scrollToLatest(false, 0);
      setTimeout(() => scrollToLatest(false, 0), 40);
      try {
        clearTransportTimers();
        wsClient.sendInput(serverId, agentId, `${text}\n`);
        startPendingAssistantMessage(text, terminalBaselineLines);
        sendingResetTimerRef.current = setTimeout(() => {
          sendingResetTimerRef.current = null;
          unlockSend();
        }, 520);
        refreshTimerRef.current = setTimeout(() => {
          refreshTimerRef.current = null;
          void refreshConversation(false);
        }, 650);
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
      clearTransportTimers,
      markNewChatMessageStarted,
      refreshConversation,
      removePendingUserMessage,
      scrollToLatest,
      serverId,
      setAttachments,
      setDraft,
      startPendingAssistantMessage,
      terminalBaselineLines,
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
    setAttachments([]);
    try {
      clearTransportTimers();
      resetForNewChat();
      wsClient.sendInput(serverId, agentId, `${submittedText}\n`);
      scrollToLatest(false, 0);
      setTimeout(() => scrollToLatest(false, 0), 40);
      sendingResetTimerRef.current = setTimeout(() => {
        sendingResetTimerRef.current = null;
        unlockSend();
        setStartingNewChat(false);
        markNewChatReady();
      }, 180);
      refreshTimerRef.current = setTimeout(() => {
        refreshTimerRef.current = null;
        void refreshConversation(false);
      }, 240);
    } catch (err: any) {
      clearTransportTimers();
      sendLockedRef.current = false;
      setSending(false);
      setStartingNewChat(false);
      Alert.alert("Command not sent", err?.message || "Could not start a new Codex chat.");
    }
  }, [
    agentId,
    clearTransportTimers,
    markNewChatReady,
    refreshConversation,
    resetForNewChat,
    scrollToLatest,
    serverId,
    setAttachments,
    setDraft,
    unlockSend,
  ]);

  const sendSlashCommandToCodex = useCallback(
    (text: string) => {
      if (sendLockedRef.current) {
        return;
      }
      sendLockedRef.current = true;
      setSending(true);
      Keyboard.dismiss();
      setDraft("");
      setAttachments([]);
      scrollToLatest(false, 0);

      try {
        clearTransportTimers();
        wsClient.sendInput(serverId, agentId, `${text}\n`);
        if (text.trimStart().startsWith("/")) {
          startPendingAssistantMessage(text, terminalBaselineLines);
        }
        sendingResetTimerRef.current = setTimeout(() => {
          sendingResetTimerRef.current = null;
          unlockSend();
        }, 420);
        refreshTimerRef.current = setTimeout(() => {
          refreshTimerRef.current = null;
          void refreshConversation(false).finally(unlockSend);
        }, 760);
      } catch (err: any) {
        clearTransportTimers();
        unlockSend();
        Alert.alert("Command not sent", err?.message || "Could not send this command to Codex.");
      }
    },
    [
      agentId,
      clearTransportTimers,
      refreshConversation,
      scrollToLatest,
      serverId,
      setAttachments,
      setDraft,
      startPendingAssistantMessage,
      terminalBaselineLines,
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
        void refreshConversation(false).finally(() => {
          setInterrupting(false);
          setSending(false);
        });
      }, 600);
    } catch {
      setInterrupting(false);
      setSending(false);
    }
  }, [agentId, connectionState, refreshConversation, sending, serverId]);

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
