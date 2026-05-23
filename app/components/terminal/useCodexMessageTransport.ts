import {
  useCallback,
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

  const submitTextToCodex = useCallback(
    (
      text: string,
      previousDraft: string,
      previousAttachments: ComposerAttachment[],
    ) => {
      const pendingMessageId = addPendingUserMessage({
        body: previousDraft.trim(),
        sentText: text,
        attachments: previousAttachments.map((attachment) => ({
          name: attachment.name,
          path: attachment.path,
        })),
      });
      setSending(true);
      scrollToLatest(false, 0);
      setDraft("");
      setAttachments([]);
      try {
        wsClient.sendInput(serverId, agentId, `${text}\n`);
        setTimeout(() => {
          void refreshConversation(false).finally(() => setSending(false));
        }, 600);
      } catch (err: any) {
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
