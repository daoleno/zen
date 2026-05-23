import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type SetStateAction,
} from "react";
import { Alert } from "react-native";
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
  refreshConversation,
  scrollToLatest,
}: UseCodexMessageTransportInput) {
  const [sending, setSending] = useState(false);
  const [interrupting, setInterrupting] = useState(false);
  const sendLockedRef = useRef(false);
  const sendingResetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const refreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (sendingResetTimerRef.current) {
        clearTimeout(sendingResetTimerRef.current);
      }
      if (refreshTimerRef.current) {
        clearTimeout(refreshTimerRef.current);
      }
      sendLockedRef.current = false;
    },
    [],
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
        wsClient.sendInput(serverId, agentId, `${text}\n`);
        if (sendingResetTimerRef.current) {
          clearTimeout(sendingResetTimerRef.current);
        }
        if (refreshTimerRef.current) {
          clearTimeout(refreshTimerRef.current);
        }
        sendingResetTimerRef.current = setTimeout(() => {
          sendingResetTimerRef.current = null;
          sendLockedRef.current = false;
          setSending(false);
        }, 220);
        refreshTimerRef.current = setTimeout(() => {
          refreshTimerRef.current = null;
          void refreshConversation(false);
        }, 650);
      } catch (err: any) {
        if (sendingResetTimerRef.current) {
          clearTimeout(sendingResetTimerRef.current);
          sendingResetTimerRef.current = null;
        }
        if (refreshTimerRef.current) {
          clearTimeout(refreshTimerRef.current);
          refreshTimerRef.current = null;
        }
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
      refreshConversation,
      removePendingUserMessage,
      scrollToLatest,
      serverId,
      setAttachments,
      setDraft,
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
    submitTextToCodex,
    interruptCodex,
  };
}
